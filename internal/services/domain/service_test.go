package domainservice

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/cloudflare"
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/constant"
	"tunnelmanager/internal/pkg/crypto"
	"tunnelmanager/internal/pkg/portalloc"
	"tunnelmanager/internal/pkg/process"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
)

type fakeDomainRepo struct {
	domains          map[string]*model.Domain
	createErr        error
	replaceRoutesErr error
	deleteErr        error
}

func newFakeDomainRepo(domains ...*model.Domain) *fakeDomainRepo {
	r := &fakeDomainRepo{domains: make(map[string]*model.Domain)}
	for _, domain := range domains {
		copy := *domain
		r.domains[domain.ID] = &copy
	}
	return r
}

func (r *fakeDomainRepo) Create(_ context.Context, domain *model.Domain) error {
	if r.createErr != nil {
		return r.createErr
	}
	copy := *domain
	r.domains[domain.ID] = &copy
	return nil
}

func (r *fakeDomainRepo) List(context.Context, domainrequest.ListDomainRequest) ([]*model.Domain, string, error) {
	return nil, "", nil
}

func (r *fakeDomainRepo) ListAll(context.Context, ...constant.DomainStatus) ([]*model.Domain, error) {
	result := make([]*model.Domain, 0, len(r.domains))
	for _, domain := range r.domains {
		copy := *domain
		result = append(result, &copy)
	}
	return result, nil
}

func (r *fakeDomainRepo) Get(_ context.Context, id string) (*model.Domain, error) {
	domain, ok := r.domains[id]
	if !ok {
		return nil, model.ErrNotFound
	}
	copy := *domain
	return &copy, nil
}

func (r *fakeDomainRepo) GetByHostname(_ context.Context, hostname string) (*model.Domain, error) {
	for _, domain := range r.domains {
		if domain.Hostname == hostname {
			copy := *domain
			return &copy, nil
		}
	}
	return nil, model.ErrNotFound
}

func (r *fakeDomainRepo) ListRoutes(_ context.Context, domainID string) ([]model.DomainRoute, error) {
	domain, ok := r.domains[domainID]
	if !ok {
		return nil, model.ErrNotFound
	}
	return append([]model.DomainRoute(nil), domain.Routes...), nil
}

func (r *fakeDomainRepo) ListRoutesByDomainIDs(_ context.Context, domainIDs []string) (map[string][]model.DomainRoute, error) {
	result := make(map[string][]model.DomainRoute, len(domainIDs))
	for _, id := range domainIDs {
		if domain := r.domains[id]; domain != nil {
			result[id] = append([]model.DomainRoute(nil), domain.Routes...)
		}
	}
	return result, nil
}

func (r *fakeDomainRepo) ReplaceRoutes(_ context.Context, domainID, defaultOriginURL string, routes []model.DomainRoute) error {
	if r.replaceRoutesErr != nil {
		return r.replaceRoutesErr
	}
	domain, ok := r.domains[domainID]
	if !ok {
		return model.ErrNotFound
	}
	domain.OriginURL = defaultOriginURL
	domain.Routes = append([]model.DomainRoute(nil), routes...)
	return nil
}

func (r *fakeDomainRepo) Update(_ context.Context, domain *model.Domain) error {
	copy := *domain
	r.domains[domain.ID] = &copy
	return nil
}

func (r *fakeDomainRepo) UpdateBulk(context.Context, []*model.Domain) error { return nil }

func (r *fakeDomainRepo) Delete(_ context.Context, id string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	delete(r.domains, id)
	return nil
}

func (r *fakeDomainRepo) ListTakenPorts(context.Context) (map[int]bool, error) {
	return map[int]bool{}, nil
}

type fakeCloudflareClient struct {
	zones              []model.CloudflareZone
	listErr            error
	createTunnelCalls  int
	createDNSZoneID    string
	deleteDNSZoneID    string
	deletedDNSRecordID string
	deletedTunnelID    string
	ingressCalls       [][]cloudflare.IngressRule
	ingressErr         error
}

func (f *fakeCloudflareClient) ListZones(context.Context) ([]model.CloudflareZone, error) {
	return f.zones, f.listErr
}

func (f *fakeCloudflareClient) CreateTunnel(context.Context, string) (cloudflare.TunnelInfo, error) {
	f.createTunnelCalls++
	return cloudflare.TunnelInfo{TunnelID: "tunnel-1", Token: "token-1"}, nil
}

func (f *fakeCloudflareClient) PutIngressConfig(_ context.Context, _, _ string, rules []cloudflare.IngressRule) error {
	f.ingressCalls = append(f.ingressCalls, append([]cloudflare.IngressRule(nil), rules...))
	return f.ingressErr
}

func (f *fakeCloudflareClient) CreateDNSRecord(_ context.Context, zoneID, _, _ string) (string, error) {
	f.createDNSZoneID = zoneID
	return "record-1", nil
}

func (f *fakeCloudflareClient) DeleteDNSRecord(_ context.Context, zoneID, recordID string) error {
	f.deleteDNSZoneID = zoneID
	f.deletedDNSRecordID = recordID
	return nil
}

func (f *fakeCloudflareClient) DeleteTunnel(_ context.Context, tunnelID string) error {
	f.deletedTunnelID = tunnelID
	return nil
}

type fakeProcessSupervisor struct {
	handler    func(process.ProcessEvent)
	startCalls int
}

type fakeIngressProxy struct {
	prepared   int
	committed  int
	rolledBack int
	removed    int
}

func (*fakeIngressProxy) URL() string                    { return "http://127.0.0.1:20080" }
func (*fakeIngressProxy) Start() error                   { return nil }
func (*fakeIngressProxy) Shutdown(context.Context) error { return nil }
func (f *fakeIngressProxy) PrepareDomain(string, string, []model.DomainRoute, []model.DomainRoute) error {
	f.prepared++
	return nil
}
func (f *fakeIngressProxy) CommitDomain(string, string, []model.DomainRoute) error {
	f.committed++
	return nil
}
func (f *fakeIngressProxy) RollbackDomain(string, string, []model.DomainRoute) error {
	f.rolledBack++
	return nil
}
func (f *fakeIngressProxy) RemoveDomain(string) { f.removed++ }

func (f *fakeProcessSupervisor) Start(string, string, int, io.Writer) error {
	f.startCalls++
	return nil
}
func (f *fakeProcessSupervisor) Stop(string) error     { return nil }
func (f *fakeProcessSupervisor) IsRunning(string) bool { return false }
func (f *fakeProcessSupervisor) SetEventHandler(handler func(process.ProcessEvent)) {
	f.handler = handler
}

func newTestDomainService(t *testing.T, repo *fakeDomainRepo, cf *fakeCloudflareClient) *domainService {
	t.Helper()
	supervisor := &fakeProcessSupervisor{}
	service := NewDomainService(DomainServiceParams{
		Cfg: config.Config{
			EncryptionKey: make([]byte, 32),
			LogDir:        t.TempDir(),
		},
		Repo:       repo,
		CF:         cf,
		Supervisor: supervisor,
		Proxy:      &fakeIngressProxy{},
		Ports: portalloc.NewPortAllocator(config.Config{
			MetricsPortRangeStart: 20500,
			MetricsPortRangeEnd:   20501,
		}),
	}, supervisor)
	domainService := service.(*domainService)
	t.Cleanup(func() {
		domainService.mu.Lock()
		defer domainService.mu.Unlock()
		for _, buf := range domainService.logs {
			_ = buf.Close()
		}
	})
	return domainService
}

func TestValidateZoneHostname(t *testing.T) {
	zones := []model.CloudflareZone{{ID: "zone-1", Name: "example.com", Status: "active"}}
	for _, test := range []struct {
		name     string
		hostname string
		want     string
		wantErr  bool
	}{
		{name: "apex", hostname: "EXAMPLE.COM.", want: "example.com"},
		{name: "subdomain", hostname: " App.Example.com. ", want: "app.example.com"},
		{name: "false suffix", hostname: "notexample.com", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateZoneHostname(zones, "zone-1", test.hostname)
			if test.wantErr != errors.Is(err, ErrInvalidZone) {
				t.Fatalf("err = %v", err)
			}
			if got != test.want {
				t.Fatalf("hostname = %q, want %q", got, test.want)
			}
		})
	}

	if _, err := validateZoneHostname([]model.CloudflareZone{{ID: "zone-1", Name: "example.com", Status: "pending"}}, "zone-1", "app.example.com"); !errors.Is(err, ErrInvalidZone) {
		t.Fatalf("inactive zone err = %v", err)
	}
	if _, err := validateZoneHostname(zones, "zone-missing", "app.example.com"); !errors.Is(err, ErrInvalidZone) {
		t.Fatalf("unknown zone err = %v", err)
	}
}

func TestCreateDomainPersistsAndUsesSelectedZone(t *testing.T) {
	repo := newFakeDomainRepo()
	cf := &fakeCloudflareClient{zones: []model.CloudflareZone{{ID: "zone-1", Name: "example.com", Status: "active"}}}
	service := newTestDomainService(t, repo, cf)

	domain, err := service.CreateDomain(context.Background(), " App.Example.com. ", "zone-1", []domainrequest.RouteInput{{Path: "/", OriginURL: "http://localhost:8080"}})
	if err != nil {
		t.Fatal(err)
	}
	if domain.Hostname != "app.example.com" || domain.CloudflareZoneID != "zone-1" {
		t.Fatalf("domain hostname/zone = %q/%q", domain.Hostname, domain.CloudflareZoneID)
	}
	if cf.createDNSZoneID != "zone-1" {
		t.Fatalf("DNS create zone = %q", cf.createDNSZoneID)
	}
	persisted := repo.domains[domain.ID]
	if persisted.CloudflareZoneID != "zone-1" {
		t.Fatalf("persisted zone = %q", persisted.CloudflareZoneID)
	}
}

func TestCreateDomainPublishesMultipleRoutes(t *testing.T) {
	repo := newFakeDomainRepo()
	cf := &fakeCloudflareClient{zones: []model.CloudflareZone{{ID: "zone-1", Name: "example.com", Status: "active"}}}
	service := newTestDomainService(t, repo, cf)

	domain, err := service.CreateDomain(t.Context(), "app.example.com", "zone-1", []domainrequest.RouteInput{
		{Path: "/api", OriginURL: "http://localhost:8080", StripPrefix: true},
		{Path: "/", OriginURL: "http://localhost:5173"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(domain.Routes) != 2 || len(cf.ingressCalls) != 1 {
		t.Fatalf("routes/ingress calls = %d/%d", len(domain.Routes), len(cf.ingressCalls))
	}
	if got := cf.ingressCalls[0][0].Service; got != "http://127.0.0.1:20080" {
		t.Fatalf("stripped route service = %q", got)
	}
	if domain.OriginURL != "http://localhost:5173" {
		t.Fatalf("legacy origin = %q", domain.OriginURL)
	}
}

func TestReplaceRoutesRestoresProxyWhenCloudflareFails(t *testing.T) {
	oldRoutes := []model.DomainRoute{{ID: "old", DomainID: "domain-1", Path: "/", OriginURL: "http://localhost:3000"}}
	repo := newFakeDomainRepo(&model.Domain{
		ID:                 "domain-1",
		Hostname:           "app.example.com",
		CloudflareTunnelID: "tunnel-1",
		OriginURL:          "http://localhost:3000",
		Routes:             oldRoutes,
	})
	cf := &fakeCloudflareClient{ingressErr: errors.New("upstream failed")}
	service := newTestDomainService(t, repo, cf)
	proxy := service.proxy.(*fakeIngressProxy)

	_, err := service.ReplaceRoutes(t.Context(), "domain-1", []domainrequest.RouteInput{{Path: "/", OriginURL: "http://localhost:4000"}})
	if !errors.Is(err, ErrCloudflareUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if proxy.prepared != 1 || proxy.rolledBack != 1 {
		t.Fatalf("proxy prepare/rollback = %d/%d", proxy.prepared, proxy.rolledBack)
	}
	if repo.domains["domain-1"].OriginURL != "http://localhost:3000" {
		t.Fatal("persisted origin changed after Cloudflare failure")
	}
}

func TestReplaceRoutesRollsBackRemoteWhenPersistenceFails(t *testing.T) {
	oldRoutes := []model.DomainRoute{{ID: "old", DomainID: "domain-1", Path: "/", OriginURL: "http://localhost:3000"}}
	repo := newFakeDomainRepo(&model.Domain{
		ID:                 "domain-1",
		Hostname:           "app.example.com",
		CloudflareTunnelID: "tunnel-1",
		OriginURL:          "http://localhost:3000",
		Routes:             oldRoutes,
	})
	repo.replaceRoutesErr = errors.New("write failed")
	cf := &fakeCloudflareClient{}
	service := newTestDomainService(t, repo, cf)
	proxy := service.proxy.(*fakeIngressProxy)

	_, err := service.ReplaceRoutes(t.Context(), "domain-1", []domainrequest.RouteInput{{Path: "/", OriginURL: "http://localhost:4000"}})
	if err == nil {
		t.Fatal("expected persistence failure")
	}
	if len(cf.ingressCalls) != 2 {
		t.Fatalf("Cloudflare ingress calls = %d, want update and rollback", len(cf.ingressCalls))
	}
	if proxy.rolledBack != 1 {
		t.Fatalf("proxy rollback calls = %d", proxy.rolledBack)
	}
}

func TestCreateDomainRejectsInvalidZoneBeforeCreatingTunnel(t *testing.T) {
	cf := &fakeCloudflareClient{zones: []model.CloudflareZone{{ID: "zone-1", Name: "example.com", Status: "active"}}}
	service := newTestDomainService(t, newFakeDomainRepo(), cf)

	_, err := service.CreateDomain(context.Background(), "app.other.com", "zone-1", []domainrequest.RouteInput{{Path: "/", OriginURL: "http://localhost:8080"}})
	if !errors.Is(err, ErrInvalidZone) || cf.createTunnelCalls != 0 {
		t.Fatalf("err/calls = %v/%d", err, cf.createTunnelCalls)
	}
}

func TestCreateDomainMapsZoneListFailure(t *testing.T) {
	service := newTestDomainService(t, newFakeDomainRepo(), &fakeCloudflareClient{listErr: errors.New("token-secret upstream failure")})
	_, err := service.CreateDomain(context.Background(), "app.example.com", "zone-1", []domainrequest.RouteInput{{Path: "/", OriginURL: "http://localhost:8080"}})
	if !errors.Is(err, ErrCloudflareUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateDomainRollbackUsesSelectedZone(t *testing.T) {
	repo := newFakeDomainRepo()
	repo.createErr = errors.New("write failed")
	cf := &fakeCloudflareClient{zones: []model.CloudflareZone{{ID: "zone-1", Name: "example.com", Status: "active"}}}
	service := newTestDomainService(t, repo, cf)

	_, err := service.CreateDomain(context.Background(), "app.example.com", "zone-1", []domainrequest.RouteInput{{Path: "/", OriginURL: "http://localhost:8080"}})
	if err == nil {
		t.Fatal("expected create failure")
	}
	if cf.deleteDNSZoneID != "zone-1" || cf.deletedDNSRecordID != "record-1" || cf.deletedTunnelID != "tunnel-1" {
		t.Fatalf("rollback zone/record/tunnel = %q/%q/%q", cf.deleteDNSZoneID, cf.deletedDNSRecordID, cf.deletedTunnelID)
	}
}

func TestDeleteDomainUsesPersistedZone(t *testing.T) {
	repo := newFakeDomainRepo(&model.Domain{ID: "domain-1", CloudflareZoneID: "zone-2", DNSRecordID: "record-2", CloudflareTunnelID: "tunnel-2"})
	cf := &fakeCloudflareClient{}
	service := newTestDomainService(t, repo, cf)

	if err := service.DeleteDomain(context.Background(), "domain-1"); err != nil {
		t.Fatal(err)
	}
	if cf.deleteDNSZoneID != "zone-2" || cf.deletedDNSRecordID != "record-2" {
		t.Fatalf("delete zone/record = %q/%q", cf.deleteDNSZoneID, cf.deletedDNSRecordID)
	}
}

func TestDeleteDomainClosesAndRemovesLogBuffer(t *testing.T) {
	repo := newFakeDomainRepo(&model.Domain{ID: "domain-1"})
	service := newTestDomainService(t, repo, &fakeCloudflareClient{})
	if _, err := service.logBuffer("domain-1"); err != nil {
		t.Fatal(err)
	}

	if err := service.DeleteDomain(t.Context(), "domain-1"); err != nil {
		t.Fatal(err)
	}
	if service.logs["domain-1"] != nil {
		t.Fatal("deleted domain retained its log buffer")
	}
}

func TestDeleteDomainRestoresProxyWhenPersistenceFails(t *testing.T) {
	routes := []model.DomainRoute{{Path: "/api", OriginURL: "http://localhost:8080", StripPrefix: true}}
	repo := newFakeDomainRepo(&model.Domain{ID: "domain-1", Hostname: "app.example.com", Routes: routes})
	repo.deleteErr = errors.New("write failed")
	service := newTestDomainService(t, repo, &fakeCloudflareClient{})
	proxy := service.proxy.(*fakeIngressProxy)

	if err := service.DeleteDomain(t.Context(), "domain-1"); err == nil {
		t.Fatal("expected delete failure")
	}
	if proxy.removed != 1 || proxy.rolledBack != 1 {
		t.Fatalf("proxy remove/rollback = %d/%d", proxy.removed, proxy.rolledBack)
	}
}

func TestReconcileHydratesRoutesBeforeStartingActiveDomains(t *testing.T) {
	key := make([]byte, 32)
	token, err := crypto.Encrypt(key, "token")
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeDomainRepo(
		&model.Domain{ID: "active", Hostname: "active.example.com", Status: constant.StatusActive, EncryptedTunnelToken: token, Routes: []model.DomainRoute{{Path: "/api", OriginURL: "http://localhost:8080", StripPrefix: true}, {Path: "/", OriginURL: "http://localhost:3000"}}},
		&model.Domain{ID: "stopped", Hostname: "stopped.example.com", Status: constant.StatusStopped, Routes: []model.DomainRoute{{Path: "/", OriginURL: "http://localhost:3000"}}},
	)
	proxy := &fakeIngressProxy{}
	supervisor := &fakeProcessSupervisor{}
	service := NewDomainService(DomainServiceParams{
		Cfg:  config.Config{EncryptionKey: key, LogDir: t.TempDir()},
		Repo: repo, CF: &fakeCloudflareClient{}, Supervisor: supervisor,
		Ports: portalloc.NewPortAllocator(config.Config{MetricsPortRangeStart: 20500, MetricsPortRangeEnd: 20501}),
		Proxy: proxy,
	}, supervisor).(*domainService)
	t.Cleanup(func() {
		service.mu.Lock()
		defer service.mu.Unlock()
		for _, buf := range service.logs {
			_ = buf.Close()
		}
	})

	if err := service.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if proxy.committed != 2 || supervisor.startCalls != 1 {
		t.Fatalf("proxy commits/process starts = %d/%d", proxy.committed, supervisor.startCalls)
	}
}

func TestReconcileDoesNotStartProcessesWhenRouteHydrationFails(t *testing.T) {
	repo := newFakeDomainRepo(&model.Domain{ID: "active", Hostname: "active.example.com", Status: constant.StatusActive})
	proxy := &fakeIngressProxy{}
	supervisor := &fakeProcessSupervisor{}
	service := NewDomainService(DomainServiceParams{
		Cfg:  config.Config{EncryptionKey: make([]byte, 32), LogDir: t.TempDir()},
		Repo: repo, CF: &fakeCloudflareClient{}, Supervisor: supervisor,
		Ports: portalloc.NewPortAllocator(config.Config{MetricsPortRangeStart: 20500, MetricsPortRangeEnd: 20501}),
		Proxy: proxy,
	}, supervisor).(*domainService)

	if err := service.Reconcile(t.Context()); !errors.Is(err, ErrInvalidRoutes) {
		t.Fatalf("err = %v", err)
	}
	if supervisor.startCalls != 0 {
		t.Fatalf("process starts = %d", supervisor.startCalls)
	}
}

func TestSubscribeLogsUsesStableBufferBeforeSpawn(t *testing.T) {
	repo := newFakeDomainRepo(&model.Domain{ID: "domain-1"})
	service := newTestDomainService(t, repo, &fakeCloudflareClient{})

	initial, updates, cancel, err := service.SubscribeLogs(t.Context(), "domain-1")
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	if len(initial) != 0 {
		t.Fatalf("initial = %#v", initial)
	}
	buf := service.logs["domain-1"]
	if buf == nil {
		t.Fatal("stable log buffer not created")
	}
	if _, err := buf.Write([]byte("connected\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("log subscriber not notified")
	}
}

func TestSubscribeLogsRejectsMissingDomain(t *testing.T) {
	service := newTestDomainService(t, newFakeDomainRepo(), &fakeCloudflareClient{})
	_, _, _, err := service.SubscribeLogs(t.Context(), "missing")
	if !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestMetricsReturnsPrometheusText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "# HELP tunnel_up\n")
	}))
	defer server.Close()

	service := newTestDomainService(t, newFakeDomainRepo(&model.Domain{
		ID:          "domain-1",
		MetricsPort: testServerPort(t, server.URL),
	}), &fakeCloudflareClient{})
	metrics, err := service.Metrics(t.Context(), "domain-1")
	if err != nil {
		t.Fatal(err)
	}
	if metrics != "# HELP tunnel_up\n" {
		t.Fatalf("metrics = %q", metrics)
	}
}

func TestMetricsPropagatesCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	service := newTestDomainService(t, newFakeDomainRepo(&model.Domain{
		ID:          "domain-1",
		MetricsPort: testServerPort(t, server.URL),
	}), &fakeCloudflareClient{})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := service.Metrics(ctx, "domain-1")
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func testServerPort(t *testing.T, rawURL string) int {
	t.Helper()
	_, portText, err := net.SplitHostPort(strings.TrimPrefix(rawURL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return port
}
