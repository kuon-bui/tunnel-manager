package domainservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/cloudflare"
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/constant"
	"tunnelmanager/internal/pkg/crypto"
	"tunnelmanager/internal/pkg/ingressproxy"
	"tunnelmanager/internal/pkg/logbuf"
	"tunnelmanager/internal/pkg/portalloc"
	"tunnelmanager/internal/pkg/process"
	domainrepo "tunnelmanager/internal/pkg/repo/domain"
	domainrequest "tunnelmanager/internal/pkg/request/domain"

	"github.com/sourcegraph/conc/pool"
	"go.uber.org/fx"
)

var (
	ErrInvalidZone           = errors.New("domainservice: invalid Cloudflare zone")
	ErrCloudflareUnavailable = errors.New("domainservice: Cloudflare unavailable")
	ErrInvalidRoutes         = errors.New("domainservice: invalid routes")
)

type DomainService interface {
	CreateDomain(ctx context.Context, hostname, zoneID string, routes []domainrequest.RouteInput) (*model.Domain, error)
	ListCloudflareZones(ctx context.Context) ([]model.CloudflareZone, error)
	ListDomains(ctx context.Context, req domainrequest.ListDomainRequest) ([]*model.Domain, string, error)
	GetDomain(ctx context.Context, id string) (*model.Domain, error)
	UpdateOrigin(ctx context.Context, id, originURL string) (*model.Domain, error)
	ReplaceRoutes(ctx context.Context, id string, routes []domainrequest.RouteInput) (*model.Domain, error)
	DeleteDomain(ctx context.Context, id string) error
	StopDomain(ctx context.Context, id string) error
	RestartDomain(ctx context.Context, id string) error
	Logs(ctx context.Context, id string) ([]string, error)
	SubscribeLogs(ctx context.Context, id string) ([]string, <-chan struct{}, func(), error)
	Metrics(ctx context.Context, id string) (string, error)
	ProxyMetrics(ctx context.Context, id string, w http.ResponseWriter) error
	Subscribe() (<-chan struct{}, func())
	HandleSupervisorEvent(ev process.ProcessEvent)
	Reconcile(ctx context.Context) error
}

type domainService struct {
	repo          domainrepo.DomainRepository
	cf            cloudflare.CloudflareClient
	sup           process.ProcessSupervisor
	ports         *portalloc.Allocator
	encKey        []byte
	logDir        string
	metricsClient *http.Client
	proxy         ingressproxy.Proxy

	mu   sync.Mutex
	logs map[string]*logbuf.Buffer

	subscriberMu sync.Mutex
	subscribers  map[chan struct{}]struct{}

	mutationMu    sync.Mutex
	mutationLocks map[string]*domainMutationLock
}

type domainMutationLock struct {
	mu   sync.Mutex
	refs int
}

type DomainServiceParams struct {
	fx.In

	Cfg        config.Config
	Repo       domainrepo.DomainRepository
	CF         cloudflare.CloudflareClient
	Supervisor process.ProcessSupervisor
	Ports      *portalloc.Allocator
	Proxy      ingressproxy.Proxy
}

func NewDomainService(
	params DomainServiceParams,
	processSupervisor process.ProcessSupervisor,
) DomainService {
	service := &domainService{
		repo:          params.Repo,
		cf:            params.CF,
		sup:           params.Supervisor,
		ports:         params.Ports,
		encKey:        params.Cfg.EncryptionKey,
		logDir:        params.Cfg.LogDir,
		metricsClient: &http.Client{Timeout: 3 * time.Second},
		proxy:         params.Proxy,
		logs:          make(map[string]*logbuf.Buffer),
		subscribers:   make(map[chan struct{}]struct{}),
		mutationLocks: make(map[string]*domainMutationLock),
	}
	processSupervisor.SetEventHandler(service.HandleSupervisorEvent)
	return service
}

func (s *domainService) lockDomain(id string) func() {
	s.mutationMu.Lock()
	lock := s.mutationLocks[id]
	if lock == nil {
		lock = &domainMutationLock{}
		s.mutationLocks[id] = lock
	}
	lock.refs++
	s.mutationMu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		s.mutationMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(s.mutationLocks, id)
		}
		s.mutationMu.Unlock()
	}
}

func (s *domainService) Logs(ctx context.Context, id string) ([]string, error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, err
	}
	s.mu.Lock()
	buf, ok := s.logs[id]
	s.mu.Unlock()
	if !ok {
		return []string{}, nil
	}
	return buf.Lines(), nil
}

func (s *domainService) logBuffer(id string) (*logbuf.Buffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if buf := s.logs[id]; buf != nil {
		return buf, nil
	}
	buf, err := logbuf.NewBuffer(filepath.Join(s.logDir, id+".log"), 500)
	if err != nil {
		return nil, err
	}
	s.logs[id] = buf
	return buf, nil
}

func (s *domainService) SubscribeLogs(ctx context.Context, id string) ([]string, <-chan struct{}, func(), error) {
	if _, err := s.repo.Get(ctx, id); err != nil {
		return nil, nil, nil, err
	}
	buf, err := s.logBuffer(id)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("service: open log buffer: %w", err)
	}
	lines, updates, cancel := buf.SnapshotAndSubscribe()
	return lines, updates, cancel, nil
}

func (s *domainService) Metrics(ctx context.Context, id string) (string, error) {
	domain, err := s.repo.Get(ctx, id)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://localhost:%d/metrics", domain.MetricsPort), nil)
	if err != nil {
		return "", fmt.Errorf("service: create metrics request: %w", err)
	}
	resp, err := s.metricsClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("service: fetch metrics: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("service: metrics endpoint returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("service: read metrics: %w", err)
	}
	return string(body), nil
}

func (s *domainService) ProxyMetrics(ctx context.Context, id string, w http.ResponseWriter) error {
	metrics, err := s.Metrics(ctx, id)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, err = io.WriteString(w, metrics)
	return err
}

func (s *domainService) Reconcile(ctx context.Context) error {
	all, err := s.repo.ListAll(ctx)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(all))
	for _, domain := range all {
		ids = append(ids, domain.ID)
	}
	routesByDomain, err := s.repo.ListRoutesByDomainIDs(ctx, ids)
	if err != nil {
		return err
	}
	active := make([]*model.Domain, 0, len(all))
	for _, domain := range all {
		routes, err := normalizePersistedRoutes(domain.ID, routesByDomain[domain.ID])
		if err != nil {
			return fmt.Errorf("service: validate routes for %s: %w", domain.ID, err)
		}
		domain.Routes = routes
		if err := s.proxy.CommitDomain(domain.ID, domain.Hostname, domain.Routes); err != nil {
			return fmt.Errorf("service: hydrate ingress proxy for %s: %w", domain.ID, err)
		}
		if domain.Status == constant.StatusActive {
			active = append(active, domain)
		}
	}

	p := pool.NewWithResults[*model.Domain]()
	for _, domain := range active {
		p.Go(func() *model.Domain {
			plaintext, err := crypto.Decrypt(s.encKey, domain.EncryptedTunnelToken)
			if err != nil {
				domain.Status = constant.StatusError
				domain.LastError = fmt.Sprintf("reconcile: decrypt token: %v", err)
				return domain
			}

			if err := s.spawn(domain, plaintext); err != nil {
				domain.Status = constant.StatusError
				domain.LastError = err.Error()
				return domain
			}
			return nil
		})
	}

	failed := make([]*model.Domain, 0, len(active))
	for _, domain := range p.Wait() {
		if domain != nil {
			failed = append(failed, domain)
		}
	}

	return s.updateBulk(ctx, failed)
}

func (s *domainService) HandleSupervisorEvent(ev process.ProcessEvent) {
	ctx := context.Background()
	domain, err := s.repo.Get(ctx, ev.DomainID)
	if err != nil {
		return
	}
	domain.Status = ev.Status
	domain.PID = ev.PID
	domain.RestartCount = ev.RestartCount
	if ev.Err != nil {
		domain.LastError = ev.Err.Error()
	}
	_ = s.update(ctx, domain)
}
