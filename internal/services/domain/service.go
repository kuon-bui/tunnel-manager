package domainservice

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/cloudflare"
	"tunnelmanager/internal/pkg/common"
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/constant"
	"tunnelmanager/internal/pkg/crypto"
	"tunnelmanager/internal/pkg/logbuf"
	"tunnelmanager/internal/pkg/portalloc"
	"tunnelmanager/internal/pkg/process"
	domainrepo "tunnelmanager/internal/pkg/repo/domain"
	domainrequest "tunnelmanager/internal/pkg/request/domain"

	"github.com/sourcegraph/conc/pool"
	"go.uber.org/fx"
)

type DomainService interface {
	CreateDomain(ctx context.Context, hostname, originURL string) (*model.Domain, error)
	ListDomains(ctx context.Context, req domainrequest.ListDomainRequest) ([]*model.Domain, string, error)
	GetDomain(ctx context.Context, id string) (*model.Domain, error)
	UpdateOrigin(ctx context.Context, id, originURL string) (*model.Domain, error)
	DeleteDomain(ctx context.Context, id string) error
	StopDomain(ctx context.Context, id string) error
	RestartDomain(ctx context.Context, id string) error
	Logs(ctx context.Context, id string) ([]string, error)
	ProxyMetrics(ctx context.Context, id string, w http.ResponseWriter) error
	Subscribe() (<-chan struct{}, func())
	HandleSupervisorEvent(ev process.ProcessEvent)
	Reconcile(ctx context.Context) error
}

type domainService struct {
	repo   domainrepo.DomainRepository
	cf     cloudflare.CloudflareClient
	sup    process.ProcessSupervisor
	ports  *portalloc.Allocator
	encKey []byte
	logDir string

	mu   sync.Mutex
	logs map[string]*logbuf.Buffer

	subscriberMu sync.Mutex
	subscribers  map[chan struct{}]struct{}
}

type DomainServiceParams struct {
	fx.In

	Cfg        config.Config
	Repo       domainrepo.DomainRepository
	CF         cloudflare.CloudflareClient
	Supervisor process.ProcessSupervisor
	Ports      *portalloc.Allocator
}

func NewDomainService(
	params DomainServiceParams,
	processSupervisor process.ProcessSupervisor,
) DomainService {
	service := &domainService{
		repo:        params.Repo,
		cf:          params.CF,
		sup:         params.Supervisor,
		ports:       params.Ports,
		encKey:      params.Cfg.EncryptionKey,
		logDir:      params.Cfg.LogDir,
		logs:        make(map[string]*logbuf.Buffer),
		subscribers: make(map[chan struct{}]struct{}),
	}
	processSupervisor.SetEventHandler(service.HandleSupervisorEvent)
	return service
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

func (s *domainService) ProxyMetrics(ctx context.Context, id string, w http.ResponseWriter) error {
	domain, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", domain.MetricsPort))
	if err != nil {
		return fmt.Errorf("service: fetch metrics: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("service: metrics endpoint returned status %d", resp.StatusCode)
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, err = io.Copy(w, resp.Body)
	return err
}

func (s *domainService) Reconcile(ctx context.Context) error {
	active, _, err := s.repo.List(ctx, domainrequest.ListDomainRequest{
		Pagination: common.Pagination{PageSize: -1},
		Status:     constant.StatusActive,
	})
	if err != nil {
		return err
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
