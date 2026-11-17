package domainservice

import (
	"errors"
	"testing"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/constant"
	"tunnelmanager/internal/pkg/crypto"
	"tunnelmanager/internal/pkg/portalloc"
)

func newStartTestService(t *testing.T, supervisor *fakeProcessSupervisor, domains ...*model.Domain) (*domainService, *fakeDomainRepo) {
	t.Helper()
	repo := newFakeDomainRepo(domains...)
	service := NewDomainService(DomainServiceParams{
		Cfg:  config.Config{EncryptionKey: make([]byte, 32), LogDir: t.TempDir()},
		Repo: repo, CF: &fakeCloudflareClient{}, Supervisor: supervisor,
		Ports: portalloc.NewPortAllocator(config.Config{MetricsPortRangeStart: 20500, MetricsPortRangeEnd: 20501}),
		Proxy: &fakeIngressProxy{},
	}, supervisor).(*domainService)
	t.Cleanup(func() {
		service.mu.Lock()
		defer service.mu.Unlock()
		for _, buf := range service.logs {
			_ = buf.Close()
		}
	})
	return service, repo
}

func stoppedDomain(t *testing.T) *model.Domain {
	t.Helper()
	token, err := crypto.Encrypt(make([]byte, 32), "token")
	if err != nil {
		t.Fatal(err)
	}
	return &model.Domain{
		ID: "domain-1", Hostname: "app.example.com", Status: constant.StatusStopped,
		EncryptedTunnelToken: token, MetricsPort: 20500, RestartCount: 3, LastError: "boom",
	}
}

func TestStartDomainSpawnsStoppedDomainAndResetsCounters(t *testing.T) {
	supervisor := &fakeProcessSupervisor{}
	service, repo := newStartTestService(t, supervisor, stoppedDomain(t))

	if err := service.StartDomain(t.Context(), "domain-1"); err != nil {
		t.Fatal(err)
	}
	if supervisor.startCalls != 1 {
		t.Fatalf("process starts = %d", supervisor.startCalls)
	}
	got := repo.domains["domain-1"]
	if got.RestartCount != 0 || got.LastError != "" {
		t.Fatalf("restartCount/lastError = %d/%q", got.RestartCount, got.LastError)
	}
}

func TestStartDomainRejectsRunningProcess(t *testing.T) {
	supervisor := &fakeProcessSupervisor{running: true}
	service, _ := newStartTestService(t, supervisor, stoppedDomain(t))

	if err := service.StartDomain(t.Context(), "domain-1"); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("err = %v", err)
	}
	if supervisor.startCalls != 0 {
		t.Fatalf("process starts = %d", supervisor.startCalls)
	}
}

func TestStartDomainReturnsNotFound(t *testing.T) {
	supervisor := &fakeProcessSupervisor{}
	service, _ := newStartTestService(t, supervisor)

	if err := service.StartDomain(t.Context(), "missing"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}
