package domainservice

import (
	"context"
	"errors"
	"io"
	"testing"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/cloudflare"
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/constant"
	"tunnelmanager/internal/pkg/portalloc"
	"tunnelmanager/internal/pkg/process"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
)

type fakeDomainRepo struct {
	domains   map[string]*model.Domain
	createErr error
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
	return nil, nil
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

func (r *fakeDomainRepo) Update(_ context.Context, domain *model.Domain) error {
	copy := *domain
	r.domains[domain.ID] = &copy
	return nil
}

func (r *fakeDomainRepo) UpdateBulk(context.Context, []*model.Domain) error { return nil }

func (r *fakeDomainRepo) Delete(_ context.Context, id string) error {
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
}

func (f *fakeCloudflareClient) ListZones(context.Context) ([]model.CloudflareZone, error) {
	return f.zones, f.listErr
}

func (f *fakeCloudflareClient) CreateTunnel(context.Context, string) (cloudflare.TunnelInfo, error) {
	f.createTunnelCalls++
	return cloudflare.TunnelInfo{TunnelID: "tunnel-1", Token: "token-1"}, nil
}

func (f *fakeCloudflareClient) PutIngressConfig(context.Context, string, string, string) error {
	return nil
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
	handler func(process.ProcessEvent)
}

func (f *fakeProcessSupervisor) Start(string, string, int, io.Writer) error { return nil }
func (f *fakeProcessSupervisor) Stop(string) error                          { return nil }
func (f *fakeProcessSupervisor) IsRunning(string) bool                      { return false }
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
		Ports: portalloc.NewPortAllocator(config.Config{
			MetricsPortRangeStart: 20500,
			MetricsPortRangeEnd:   20501,
		}),
	}, supervisor)
	return service.(*domainService)
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

	domain, err := service.CreateDomain(context.Background(), " App.Example.com. ", "http://localhost:8080", "zone-1")
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

func TestCreateDomainRejectsInvalidZoneBeforeCreatingTunnel(t *testing.T) {
	cf := &fakeCloudflareClient{zones: []model.CloudflareZone{{ID: "zone-1", Name: "example.com", Status: "active"}}}
	service := newTestDomainService(t, newFakeDomainRepo(), cf)

	_, err := service.CreateDomain(context.Background(), "app.other.com", "http://localhost:8080", "zone-1")
	if !errors.Is(err, ErrInvalidZone) || cf.createTunnelCalls != 0 {
		t.Fatalf("err/calls = %v/%d", err, cf.createTunnelCalls)
	}
}

func TestCreateDomainMapsZoneListFailure(t *testing.T) {
	service := newTestDomainService(t, newFakeDomainRepo(), &fakeCloudflareClient{listErr: errors.New("token-secret upstream failure")})
	_, err := service.CreateDomain(context.Background(), "app.example.com", "http://localhost:8080", "zone-1")
	if !errors.Is(err, ErrCloudflareUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateDomainRollbackUsesSelectedZone(t *testing.T) {
	repo := newFakeDomainRepo()
	repo.createErr = errors.New("write failed")
	cf := &fakeCloudflareClient{zones: []model.CloudflareZone{{ID: "zone-1", Name: "example.com", Status: "active"}}}
	service := newTestDomainService(t, repo, cf)

	_, err := service.CreateDomain(context.Background(), "app.example.com", "http://localhost:8080", "zone-1")
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
