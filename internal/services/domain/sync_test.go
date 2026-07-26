package domainservice

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/cloudflare"
	"tunnelmanager/internal/pkg/constant"
	"tunnelmanager/internal/pkg/process"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
)

func TestSyncCloudflareReplacesSyncedDomainsAndKeepsManagedTunnelsExcluded(t *testing.T) {
	managedTunnelID := "managed-tunnel"
	repo := &syncTestRepository{
		all: []*model.Domain{
			{CloudflareTunnelID: managedTunnelID, Managed: true},
			{CloudflareTunnelID: "old-synced-tunnel", Managed: false},
		},
	}
	cf := &syncTestCloudflare{
		tunnels: []cloudflare.RemoteTunnel{
			{
				TunnelID: managedTunnelID,
				Name:     "created-by-app",
				Status:   "healthy",
				Ingress: []cloudflare.TunnelIngress{
					{Hostname: "managed.example.com", OriginURL: "http://managed:80"},
				},
			},
			{
				TunnelID:  "remote-tunnel",
				Name:      "created-on-cloudflare",
				Status:    "healthy",
				CreatedAt: time.Date(2026, 7, 20, 1, 2, 3, 0, time.UTC),
				Ingress: []cloudflare.TunnelIngress{
					{
						Hostname:  "remote.example.com",
						OriginURL: "http://remote:8080",
						Path:      "/api/.*",
					},
					{OriginURL: "http_status:404"},
				},
			},
		},
	}

	service := &domainService{repo: repo, cf: cf}
	if err := service.SyncCloudflare(context.Background()); err != nil {
		t.Fatalf("SyncCloudflare() error = %v", err)
	}

	if len(repo.replaced) != 1 {
		t.Fatalf("ReplaceSynced() got %d domains, want 1", len(repo.replaced))
	}
	got := repo.replaced[0]
	if got.Hostname != "remote.example.com" || got.OriginURL != "http://remote:8080" {
		t.Fatalf("synced route = %q -> %q", got.Hostname, got.OriginURL)
	}
	if got.Path != "/api/.*" {
		t.Fatalf("synced path = %q, want %q", got.Path, "/api/.*")
	}
	if got.CloudflareTunnelID != "remote-tunnel" ||
		got.CloudflareTunnelName != "created-on-cloudflare" ||
		got.CloudflareStatus != "healthy" {
		t.Fatalf("synced Cloudflare metadata = %#v", got)
	}
	if got.Managed {
		t.Fatal("synced domain must not be managed")
	}
	if got.Status != constant.StatusActive || got.MetricsPort != 0 ||
		got.EncryptedTunnelToken != "" {
		t.Fatalf("synced process fields = %#v", got)
	}
}

func TestSpawnNeverStartsSupervisorForSyncedDomain(t *testing.T) {
	supervisor := &syncTestSupervisor{}
	service := &domainService{sup: supervisor}

	err := service.spawn(&model.Domain{ID: "synced", Managed: false}, "token")
	if err != model.ErrSyncedDomainReadOnly {
		t.Fatalf("spawn() error = %v, want %v", err, model.ErrSyncedDomainReadOnly)
	}
	if supervisor.startCalls != 0 {
		t.Fatalf("supervisor Start() called %d times, want 0", supervisor.startCalls)
	}
}

func TestCloudflareSyncSchedulerRunsPeriodicallyAndStops(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	called := make(chan struct{}, 1)
	scheduler := newCloudflareSyncScheduler(5*time.Millisecond, func(context.Context) error {
		mu.Lock()
		calls++
		mu.Unlock()
		select {
		case called <- struct{}{}:
		default:
		}
		return nil
	})

	scheduler.Start()
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("periodic sync was not called")
	}

	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := scheduler.Stop(stopCtx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	mu.Lock()
	callsAfterStop := calls
	mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if calls != callsAfterStop {
		t.Fatalf("sync calls after Stop(): got %d, want %d", calls, callsAfterStop)
	}
}

type syncTestCloudflare struct {
	tunnels      []cloudflare.RemoteTunnel
	tunnel       cloudflare.TunnelInfo
	putHostname  string
	putOriginURL string
	putPath      string
}

func (f *syncTestCloudflare) CreateTunnel(context.Context, string) (cloudflare.TunnelInfo, error) {
	return f.tunnel, nil
}

func (f *syncTestCloudflare) ListRemoteTunnels(context.Context) ([]cloudflare.RemoteTunnel, error) {
	return f.tunnels, nil
}

func (f *syncTestCloudflare) PutIngressConfig(
	_ context.Context,
	_ string,
	hostname string,
	originURL string,
	path string,
) error {
	f.putHostname = hostname
	f.putOriginURL = originURL
	f.putPath = path
	return nil
}

func (f *syncTestCloudflare) CreateDNSRecord(context.Context, string, string) (string, error) {
	return "", nil
}

func (f *syncTestCloudflare) DeleteDNSRecord(context.Context, string) error {
	return nil
}

func (f *syncTestCloudflare) DeleteTunnel(context.Context, string) error {
	return nil
}

type syncTestRepository struct {
	all      []*model.Domain
	replaced []*model.Domain
	created  *model.Domain
}

func (r *syncTestRepository) Create(_ context.Context, domain *model.Domain) error {
	r.created = domain
	return nil
}

func (r *syncTestRepository) List(
	context.Context,
	domainrequest.ListDomainRequest,
) ([]*model.Domain, string, error) {
	return nil, "", nil
}

func (r *syncTestRepository) ListAll(
	context.Context,
	...constant.DomainStatus,
) ([]*model.Domain, error) {
	return r.all, nil
}

func (r *syncTestRepository) Get(context.Context, string) (*model.Domain, error) {
	return nil, model.ErrNotFound
}

func (r *syncTestRepository) GetByHostname(context.Context, string) (*model.Domain, error) {
	return nil, model.ErrNotFound
}

func (r *syncTestRepository) Update(context.Context, *model.Domain) error {
	return nil
}

func (r *syncTestRepository) UpdateBulk(context.Context, []*model.Domain) error {
	return nil
}

func (r *syncTestRepository) ReplaceSynced(_ context.Context, domains []*model.Domain) error {
	r.replaced = domains
	return nil
}

func (r *syncTestRepository) Delete(context.Context, string) error {
	return nil
}

func (r *syncTestRepository) ListTakenPorts(context.Context) (map[int]bool, error) {
	return nil, nil
}

type syncTestSupervisor struct {
	startCalls int
}

func (s *syncTestSupervisor) Start(string, string, int, io.Writer) error {
	s.startCalls++
	return nil
}

func (s *syncTestSupervisor) Stop(string) error {
	return nil
}

func (s *syncTestSupervisor) IsRunning(string) bool {
	return false
}

func (s *syncTestSupervisor) SetEventHandler(func(process.ProcessEvent)) {}
