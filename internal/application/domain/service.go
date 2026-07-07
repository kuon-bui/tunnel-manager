package domain

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"

	"tunnelmanager/internal/application/ports"
	"tunnelmanager/internal/crypto"
	"tunnelmanager/internal/logbuf"
	"tunnelmanager/internal/model"
	"tunnelmanager/internal/portalloc"
	"tunnelmanager/internal/repositories"
)

type DomainService interface {
	CreateDomain(ctx context.Context, hostname, originURL string) (*model.Domain, error)
	ListDomains(ctx context.Context) ([]model.Domain, error)
	GetDomain(ctx context.Context, id string) (*model.Domain, error)
	UpdateOrigin(ctx context.Context, id, originURL string) (*model.Domain, error)
	DeleteDomain(ctx context.Context, id string) error
	StopDomain(ctx context.Context, id string) error
	RestartDomain(ctx context.Context, id string) error
	Logs(ctx context.Context, id string) ([]string, error)
	ProxyMetrics(ctx context.Context, id string, w http.ResponseWriter) error
	HandleSupervisorEvent(ev ports.ProcessEvent)
	Reconcile(ctx context.Context) error
}

type domainService struct {
	repo   repositories.DomainRepository
	cf     ports.CloudflareClient
	sup    ports.ProcessSupervisor
	ports  *portalloc.Allocator
	encKey []byte
	logDir string

	mu   sync.Mutex
	logs map[string]*logbuf.Buffer
}

func NewDomainService(
	repo repositories.DomainRepository,
	cf ports.CloudflareClient,
	sup ports.ProcessSupervisor,
	portsAllocator *portalloc.Allocator,
	encKey []byte,
	logDir string,
) DomainService {
	return &domainService{
		repo:   repo,
		cf:     cf,
		sup:    sup,
		ports:  portsAllocator,
		encKey: encKey,
		logDir: logDir,
		logs:   make(map[string]*logbuf.Buffer),
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
	active, err := s.repo.ListByStatus(ctx, model.StatusActive)
	if err != nil {
		return err
	}
	for i := range active {
		domain := &active[i]
		plaintext, err := crypto.Decrypt(s.encKey, domain.EncryptedTunnelToken)
		if err != nil {
			domain.Status = model.StatusError
			domain.LastError = fmt.Sprintf("reconcile: decrypt token: %v", err)
			_ = s.repo.Update(ctx, domain)
			continue
		}
		if err := s.spawn(domain, plaintext); err != nil {
			domain.Status = model.StatusError
			domain.LastError = err.Error()
			_ = s.repo.Update(ctx, domain)
		}
	}
	return nil
}

func (s *domainService) HandleSupervisorEvent(ev ports.ProcessEvent) {
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
	_ = s.repo.Update(ctx, domain)
}
