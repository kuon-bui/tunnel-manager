# Cloudflare Multi-Zone Domains Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let frontend list active Cloudflare zones and create managed domains in a selected zone while persisting zone ownership for later DNS cleanup.

**Architecture:** Keep one global Cloudflare API token/account and current one-hostname-per-tunnel lifecycle. Extend existing domain route/service and Cloudflare adapter; fetch zones live from Cloudflare, validate selected zone in service, and persist `cloudflare_zone_id` on each domain row.

**Tech Stack:** Go 1.26.1, Gin 1.12.0, Fx 1.24.0, Bun 1.2.18, SQLite, Goose SQL migrations, `cloudflare-go/v6` v6.10.0, Go standard `testing` package.

## Global Constraints

- Keep dependency flow `HTTP route/handler -> domain service -> Cloudflare client -> Cloudflare API`.
- Keep one `CLOUDFLARE_API_TOKEN`, one `CLOUDFLARE_ACCOUNT_ID`, one hostname per tunnel, and one local process per domain.
- `POST /api/domains` requires `zoneId`; no compatibility fallback to `CLOUDFLARE_ZONE_ID`.
- `GET /api/cloudflare/zones` requires JWT and returns every active zone sorted by normalized name.
- Do not add zone cache, zone table, sync job, multi-account support, shared tunnels, IDN conversion, or new dependencies.
- Keep hostname globally unique.
- Migration is intentionally destructive: local domain rows are discarded, not backfilled.
- Never expose Cloudflare SDK errors, API tokens, or credentials in HTTP responses.
- Use Go standard `testing`; add no test framework.

## File Map

- `internal/model/cloudflare.go`: API-safe Cloudflare zone shape.
- `internal/model/domain.go`: persisted and serialized `CloudflareZoneID`.
- `internal/pkg/request/domain/create.go`: required `zoneId` request field.
- `migrations/00004_recreate_domains_with_zone_id.sql`: destructive domain-table reset.
- `migrations/migrations_test.go`: executable schema/reset check.
- `internal/pkg/cloudflare/type.go`: zone-list and zone-scoped DNS contracts.
- `internal/pkg/cloudflare/client.go`: Cloudflare zone pagination plus explicit-zone DNS calls.
- `internal/pkg/cloudflare/client_test.go`: local HTTP checks for Cloudflare request scoping and pagination.
- `internal/services/domain/service.go`: service interface and domain-level errors.
- `internal/services/domain/zones.go`: hostname normalization, zone validation, and zone listing.
- `internal/services/domain/create.go`: selected-zone create and rollback flow.
- `internal/services/domain/delete.go`: persisted-zone DNS deletion.
- `internal/services/domain/service_test.go`: service fakes and zone behavior checks.
- `internal/application/api/route/domain/create.go`: four-argument create call and `400`/`502` mapping.
- `internal/application/api/route/domain/zones.go`: zone-list handler.
- `internal/application/api/route/domain/route.go`: authenticated `/api/cloudflare/zones` registration.
- `internal/application/api/route/domain/route_test.go`: auth, response, and error mapping checks.
- `internal/pkg/config/config.go`: remove obsolete global zone config.
- `internal/pkg/config/config_test.go`: prove startup no longer requires global zone ID.
- `.env.example`: remove `CLOUDFLARE_ZONE_ID`.
- `README.md`: permissions, API contract, destructive rollout instructions.

---

### Task 1: Persist Zone Ownership and Reset Domain Schema

**Files:**
- Create: `internal/model/cloudflare.go`
- Modify: `internal/model/domain.go`
- Modify: `internal/pkg/request/domain/create.go`
- Create: `migrations/00004_recreate_domains_with_zone_id.sql`
- Create: `migrations/migrations_test.go`

**Interfaces:**
- Produces: `model.CloudflareZone`, `model.Domain.CloudflareZoneID`, and `domainrequest.CreateDomainRequest.ZoneID`.
- Produces DB column: `domains.cloudflare_zone_id TEXT NOT NULL`.
- Consumes no new application interfaces.

- [ ] **Step 1: Write failing migration schema test**

Create `migrations/migrations_test.go`:

```go
package migrations

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDomainZoneMigrationResetsSchema(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "migration.db")+"?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, migrationSection(t, "00001_create_domains.sql", "-- +goose Up", "-- +goose Down")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO domains (
		id, hostname, origin_url, cloudflare_tunnel_id, dns_record_id,
		tunnel_token, status, metrics_port, pid, restart_count, last_error,
		created_at, updated_at
	) VALUES ('old', 'old.example.com', 'http://localhost:8080', 'tunnel',
		'record', 'token', 'stopped', 20500, 0, 0, '', CURRENT_TIMESTAMP,
		CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}

	if _, err := db.ExecContext(ctx, migrationSection(t, "00004_recreate_domains_with_zone_id.sql", "-- +goose Up", "-- +goose Down")); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM domains").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("domain count = %d, want 0", count)
	}

	rows, err := db.QueryContext(ctx, "PRAGMA table_info(domains)")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "cloudflare_zone_id" {
			found = true
			if columnType != "TEXT" || notNull != 1 {
				t.Fatalf("cloudflare_zone_id type/notnull = %s/%d", columnType, notNull)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("cloudflare_zone_id column missing")
	}
}

func migrationSection(t *testing.T, name, start, end string) string {
	t.Helper()
	contents, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	section, ok := strings.CutPrefix(string(contents), start)
	if !ok {
		t.Fatalf("%s missing %s", name, start)
	}
	section, _, ok = strings.Cut(section, end)
	if !ok {
		t.Fatalf("%s missing %s", name, end)
	}
	return section
}
```

- [ ] **Step 2: Run test to verify failure**

Run: `go test ./migrations -run TestDomainZoneMigrationResetsSchema -v`

Expected: FAIL because `00004_recreate_domains_with_zone_id.sql` does not exist.

- [ ] **Step 3: Add model and request fields**

Create `internal/model/cloudflare.go`:

```go
package model

type CloudflareZone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}
```

Add this field after `OriginURL` in `internal/model/domain.go`:

```go
CloudflareZoneID string `bun:"cloudflare_zone_id,notnull" json:"zoneId"`
```

Replace `internal/pkg/request/domain/create.go` with:

```go
package domainrequest

type CreateDomainRequest struct {
	Hostname  string `json:"hostname" binding:"required"`
	OriginURL string `json:"originUrl" binding:"required"`
	ZoneID    string `json:"zoneId" binding:"required"`
}
```

- [ ] **Step 4: Add destructive migration**

Create `migrations/00004_recreate_domains_with_zone_id.sql`:

```sql
-- +goose Up
DROP TABLE domains;
CREATE TABLE domains (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL UNIQUE,
    origin_url TEXT NOT NULL,
    cloudflare_zone_id TEXT NOT NULL,
    cloudflare_tunnel_id TEXT NOT NULL,
    dns_record_id TEXT NOT NULL,
    tunnel_token TEXT NOT NULL,
    status TEXT NOT NULL,
    metrics_port INTEGER NOT NULL,
    pid INTEGER NOT NULL DEFAULT 0,
    restart_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

-- +goose Down
DROP TABLE domains;
CREATE TABLE domains (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL UNIQUE,
    origin_url TEXT NOT NULL,
    cloudflare_tunnel_id TEXT NOT NULL,
    dns_record_id TEXT NOT NULL,
    tunnel_token TEXT NOT NULL,
    status TEXT NOT NULL,
    metrics_port INTEGER NOT NULL,
    pid INTEGER NOT NULL DEFAULT 0,
    restart_count INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
```

- [ ] **Step 5: Run focused and full tests**

Run: `go test ./migrations -run TestDomainZoneMigrationResetsSchema -v`

Expected: PASS; old row count is zero and `cloudflare_zone_id` is `TEXT NOT NULL`.

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 6: Commit data contract**

```bash
git add internal/model/cloudflare.go internal/model/domain.go internal/pkg/request/domain/create.go migrations/00004_recreate_domains_with_zone_id.sql migrations/migrations_test.go
git commit -m "feat: persist Cloudflare zone ownership"
```

---

### Task 2: List Active Account Zones Through Cloudflare Adapter

**Files:**
- Modify: `internal/pkg/cloudflare/type.go`
- Modify: `internal/pkg/cloudflare/client.go`
- Create: `internal/pkg/cloudflare/client_test.go`

**Interfaces:**
- Consumes: `model.CloudflareZone` from Task 1.
- Produces: `CloudflareClient.ListZones(ctx context.Context) ([]model.CloudflareZone, error)`.
- Keeps existing DNS method signatures until Task 3.

- [ ] **Step 1: Write failing zone pagination test**

Create `internal/pkg/cloudflare/client_test.go`:

```go
package cloudflare

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	cloudflareapi "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/option"
)

func TestListZonesFetchesAllPagesAndReturnsSortedActiveZones(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/zones" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("account.id"); got != "account-1" {
			t.Fatalf("account.id = %q", got)
		}
		if got := r.URL.Query().Get("status"); got != "active" {
			t.Fatalf("status = %q", got)
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":[{"id":"zone-b","name":"B.EXAMPLE.","status":"active"},{"id":"zone-x","name":"ignored.example","status":"pending"}],"result_info":{"page":1,"per_page":50}}`)
		case "2":
			fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":[{"id":"zone-a","name":"a.example","status":"active"}],"result_info":{"page":2,"per_page":50}}`)
		default:
			fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":[],"result_info":{"page":3,"per_page":50}}`)
		}
	}))
	defer server.Close()

	client := &client{
		api: cloudflareapi.NewClient(
			option.WithBaseURL(server.URL),
			option.WithAPIToken("test-token"),
		),
		accountID: "account-1",
	}

	got, err := client.ListZones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"zone-a:a.example:active", "zone-b:b.example:active"}
	formatted := make([]string, 0, len(got))
	for _, zone := range got {
		formatted = append(formatted, zone.ID+":"+zone.Name+":"+zone.Status)
	}
	if !reflect.DeepEqual(formatted, want) {
		t.Fatalf("zones = %#v, want %#v", formatted, want)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
}
```

- [ ] **Step 2: Run test to verify interface failure**

Run: `go test ./internal/pkg/cloudflare -run TestListZonesFetchesAllPagesAndReturnsSortedActiveZones -v`

Expected: FAIL with `client.ListZones undefined`.

- [ ] **Step 3: Extend adapter interface**

Add imports and method to `internal/pkg/cloudflare/type.go`:

```go
import (
	"context"
	"tunnelmanager/internal/model"
)
```

```go
ListZones(ctx context.Context) ([]model.CloudflareZone, error)
```

- [ ] **Step 4: Implement live active-zone listing**

Add imports to `internal/pkg/cloudflare/client.go`:

```go
"sort"
"strings"
"tunnelmanager/internal/model"
"github.com/cloudflare/cloudflare-go/v6/zones"
```

Add method:

```go
func (c *client) ListZones(ctx context.Context) ([]model.CloudflareZone, error) {
	pager := c.api.Zones.ListAutoPaging(ctx, zones.ZoneListParams{
		Account: cloudflareapi.F(zones.ZoneListParamsAccount{
			ID: cloudflareapi.F(c.accountID),
		}),
		Page:    cloudflareapi.F(float64(1)),
		PerPage: cloudflareapi.F(float64(50)),
		Status:  cloudflareapi.F(zones.ZoneListParamsStatusActive),
	})

	result := make([]model.CloudflareZone, 0)
	for pager.Next() {
		zone := pager.Current()
		if zone.Status != zones.ZoneStatusActive {
			continue
		}
		result = append(result, model.CloudflareZone{
			ID:     zone.ID,
			Name:   strings.TrimSuffix(strings.ToLower(strings.TrimSpace(zone.Name)), "."),
			Status: string(zone.Status),
		})
	}
	if err := pager.Err(); err != nil {
		return nil, fmt.Errorf("cloudflare: list zones: %w", err)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, nil
}
```

- [ ] **Step 5: Run adapter tests**

Run: `go test ./internal/pkg/cloudflare -run TestListZonesFetchesAllPagesAndReturnsSortedActiveZones -v`

Expected: PASS with three HTTP requests and sorted active zones.

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 6: Commit zone listing adapter**

```bash
git add internal/pkg/cloudflare/type.go internal/pkg/cloudflare/client.go internal/pkg/cloudflare/client_test.go
git commit -m "feat: list active Cloudflare zones"
```

---

### Task 3: Validate Selected Zone and Scope DNS Lifecycle

**Files:**
- Modify: `internal/pkg/cloudflare/type.go`
- Modify: `internal/pkg/cloudflare/client.go`
- Modify: `internal/pkg/cloudflare/client_test.go`
- Modify: `internal/services/domain/service.go`
- Create: `internal/services/domain/zones.go`
- Modify: `internal/services/domain/create.go`
- Modify: `internal/services/domain/delete.go`
- Modify: `internal/application/api/route/domain/create.go`
- Create: `internal/services/domain/service_test.go`

**Interfaces:**
- Consumes: active zones from `CloudflareClient.ListZones`.
- Changes: `CreateDNSRecord(ctx, zoneID, hostname, tunnelID string) (string, error)`.
- Changes: `DeleteDNSRecord(ctx, zoneID, dnsRecordID string) error`.
- Changes: `DomainService.CreateDomain(ctx, hostname, originURL, zoneID string) (*model.Domain, error)`.
- Produces: `DomainService.ListCloudflareZones(ctx) ([]model.CloudflareZone, error)`.
- Produces sentinels: `ErrInvalidZone` and `ErrCloudflareUnavailable`.

- [ ] **Step 1: Write failing service behavior tests**

Create `internal/services/domain/service_test.go` with package-local fakes and focused tests:

```go
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
	zones               []model.CloudflareZone
	listErr             error
	createTunnelCalls   int
	createDNSZoneID     string
	deleteDNSZoneID     string
	deletedDNSRecordID  string
	deletedTunnelID     string
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
func (f *fakeProcessSupervisor) Stop(string) error                         { return nil }
func (f *fakeProcessSupervisor) IsRunning(string) bool                     { return false }
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
```

- [ ] **Step 2: Run service test to verify production API failure**

Run: `go test ./internal/services/domain -run 'TestValidateZoneHostname|TestCreateDomain|TestDeleteDomain' -v`

Expected: FAIL because `validateZoneHostname`, error sentinels, four-argument `CreateDomain`, and explicit-zone DNS methods do not exist.

- [ ] **Step 3: Add service errors, zone listing, normalization, and validation**

In `internal/services/domain/service.go`, add:

```go
var (
	ErrInvalidZone          = errors.New("domainservice: invalid Cloudflare zone")
	ErrCloudflareUnavailable = errors.New("domainservice: Cloudflare unavailable")
)
```

Add `errors` import and these interface signatures:

```go
CreateDomain(ctx context.Context, hostname, originURL, zoneID string) (*model.Domain, error)
ListCloudflareZones(ctx context.Context) ([]model.CloudflareZone, error)
```

Create `internal/services/domain/zones.go`:

```go
package domainservice

import (
	"context"
	"fmt"
	"strings"

	"tunnelmanager/internal/model"
)

func (s *domainService) ListCloudflareZones(ctx context.Context) ([]model.CloudflareZone, error) {
	zones, err := s.cf.ListZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: list zones: %w", ErrCloudflareUnavailable, err)
	}
	return zones, nil
}

func validateZoneHostname(zones []model.CloudflareZone, zoneID, hostname string) (string, error) {
	normalizedHostname := normalizeDNSName(hostname)
	for _, zone := range zones {
		if zone.ID != zoneID || zone.Status != "active" {
			continue
		}
		zoneName := normalizeDNSName(zone.Name)
		if normalizedHostname == zoneName || strings.HasSuffix(normalizedHostname, "."+zoneName) {
			return normalizedHostname, nil
		}
		break
	}
	return "", ErrInvalidZone
}

func normalizeDNSName(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}
```

- [ ] **Step 4: Make DNS adapter explicitly zone-scoped**

Change `internal/pkg/cloudflare/type.go` methods to:

```go
CreateDNSRecord(ctx context.Context, zoneID, hostname, tunnelID string) (dnsRecordID string, err error)
DeleteDNSRecord(ctx context.Context, zoneID, dnsRecordID string) error
```

In `internal/pkg/cloudflare/client.go`:

- Remove `zoneID string` from `client`.
- Remove `zoneID: cfg.CloudflareZoneID` from `NewCloudflareClient`.
- Change DNS methods to accept `zoneID` and use that argument in `dns.RecordNewParams.ZoneID` and `dns.RecordDeleteParams.ZoneID`.

Resulting signatures:

```go
func (c *client) CreateDNSRecord(ctx context.Context, zoneID, hostname, tunnelID string) (string, error)
func (c *client) DeleteDNSRecord(ctx context.Context, zoneID, dnsRecordID string) error
```

Append adapter request-scope test to `internal/pkg/cloudflare/client_test.go`:

```go
func TestDNSMethodsUseExplicitZoneID(t *testing.T) {
	requests := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":{"id":"record-1","name":"app.example.com","type":"CNAME","content":"tunnel-1.cfargotunnel.com","ttl":1,"proxied":true}}`)
			return
		}
		fmt.Fprint(w, `{"success":true,"errors":[],"messages":[],"result":{"id":"record-1"}}`)
	}))
	defer server.Close()

	client := &client{api: cloudflareapi.NewClient(option.WithBaseURL(server.URL), option.WithAPIToken("test-token")), accountID: "account-1"}
	recordID, err := client.CreateDNSRecord(context.Background(), "zone-create", "app.example.com", "tunnel-1")
	if err != nil {
		t.Fatal(err)
	}
	if recordID != "record-1" {
		t.Fatalf("record ID = %q", recordID)
	}
	if err := client.DeleteDNSRecord(context.Background(), "zone-delete", "record-1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"POST /zones/zone-create/dns_records", "DELETE /zones/zone-delete/dns_records/record-1"}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}
```

- [ ] **Step 5: Thread selected zone through create and rollback**

Change `CreateDomain` signature in `internal/services/domain/create.go`:

```go
func (s *domainService) CreateDomain(ctx context.Context, hostname, originURL, zoneID string) (domain *model.Domain, err error)
```

At function start, before duplicate lookup:

```go
zones, err := s.ListCloudflareZones(ctx)
if err != nil {
	return nil, err
}
hostname, err = validateZoneHostname(zones, zoneID, hostname)
if err != nil {
	return nil, err
}
```

Change DNS create and rollback:

```go
dnsRecordID, err := s.cf.CreateDNSRecord(ctx, zoneID, hostname, tunnel.TunnelID)
```

```go
_ = s.cf.DeleteDNSRecord(ctx, zoneID, dnsRecordID)
```

Persist selected zone in model literal:

```go
CloudflareZoneID: zoneID,
```

- [ ] **Step 6: Use persisted zone during delete**

Change `internal/services/domain/delete.go`:

```go
if err := s.cf.DeleteDNSRecord(ctx, domain.CloudflareZoneID, domain.DNSRecordID); err != nil {
	log.Printf("service: delete domain %s: delete dns record %s failed, continuing with best-effort cleanup: %v", id, domain.DNSRecordID, err)
}
```

- [ ] **Step 7: Keep create handler compiling and map zone-specific errors**

Replace `internal/application/api/route/domain/create.go` with:

```go
package domainroute

import (
	"errors"
	"net/http"

	domainrequest "tunnelmanager/internal/pkg/request/domain"
	domainservice "tunnelmanager/internal/services/domain"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) createDomain(c *gin.Context) {
	var req domainrequest.CreateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	domain, err := h.domainService.CreateDomain(c.Request.Context(), req.Hostname, req.OriginURL, req.ZoneID)
	if err != nil {
		if errors.Is(err, domainservice.ErrCloudflareUnavailable) {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Cloudflare unavailable"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, domain)
}
```

- [ ] **Step 8: Run core tests**

Run: `go test ./internal/pkg/cloudflare ./internal/services/domain -v`

Expected: PASS, including explicit zone paths, validation, persistence, rollback, and delete.

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 9: Commit zone-scoped lifecycle**

```bash
git add internal/pkg/cloudflare/type.go internal/pkg/cloudflare/client.go internal/pkg/cloudflare/client_test.go internal/services/domain/service.go internal/services/domain/zones.go internal/services/domain/create.go internal/services/domain/delete.go internal/services/domain/service_test.go internal/application/api/route/domain/create.go
git commit -m "feat: scope domain DNS by zone"
```

---

### Task 4: Expose Authenticated Zone List API

**Files:**
- Create: `internal/application/api/route/domain/zones.go`
- Modify: `internal/application/api/route/domain/route.go`
- Create: `internal/application/api/route/domain/route_test.go`

**Interfaces:**
- Consumes: `DomainService.ListCloudflareZones(ctx)` from Task 3.
- Produces: JWT-protected `GET /api/cloudflare/zones`.
- Produces response: `{"items":[{"id":"...","name":"...","status":"active"}]}`.

- [ ] **Step 1: Write failing route tests**

Create `internal/application/api/route/domain/route_test.go`:

```go
package domainroute

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tunnelmanager/internal/model"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
	authservice "tunnelmanager/internal/services/auth"
	domainservice "tunnelmanager/internal/services/domain"

	"github.com/gin-gonic/gin"
)

type fakeRouteDomainService struct {
	domainservice.DomainService
	zones    []model.CloudflareZone
	zonesErr error
	created  *model.Domain
	domains  []*model.Domain
}

func (f *fakeRouteDomainService) ListCloudflareZones(context.Context) ([]model.CloudflareZone, error) {
	return f.zones, f.zonesErr
}
func (f *fakeRouteDomainService) CreateDomain(context.Context, string, string, string) (*model.Domain, error) {
	return f.created, f.zonesErr
}
func (f *fakeRouteDomainService) GetDomain(context.Context, string) (*model.Domain, error) {
	return f.domains[0], nil
}
func (f *fakeRouteDomainService) ListDomains(context.Context, domainrequest.ListDomainRequest) ([]*model.Domain, string, error) {
	return f.domains, "", nil
}

type fakeRouteAuthService struct {
	authservice.AuthService
}

func (f *fakeRouteAuthService) Authenticate(context.Context, string) (string, error) {
	return "admin", nil
}

func TestZoneListRequiresJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := &DomainHandler{domainService: &fakeRouteDomainService{}}
	route := &DomainRoute{Engine: engine, domainHandler: handler, authService: &fakeRouteAuthService{}}
	route.Setup()

	request := httptest.NewRequest(http.MethodGet, "/api/cloudflare/zones", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestListCloudflareZonesResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := &DomainHandler{domainService: &fakeRouteDomainService{zones: []model.CloudflareZone{{ID: "zone-1", Name: "example.com", Status: "active"}}}}
	engine.GET("/api/cloudflare/zones", handler.listCloudflareZones)

	request := httptest.NewRequest(http.MethodGet, "/api/cloudflare/zones", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"items":[{"id":"zone-1","name":"example.com","status":"active"}]`) {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}

func TestListCloudflareZonesHidesUpstreamError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := &DomainHandler{domainService: &fakeRouteDomainService{zonesErr: errors.New("token-secret upstream failure")}}
	engine.GET("/api/cloudflare/zones", handler.listCloudflareZones)

	request := httptest.NewRequest(http.MethodGet, "/api/cloudflare/zones", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "token-secret") {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}

func TestCreateDomainRequiresZoneIDAndReturnsIt(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeRouteDomainService{created: &model.Domain{ID: "domain-1", CloudflareZoneID: "zone-1"}}
	engine := gin.New()
	handler := &DomainHandler{domainService: service}
	engine.POST("/api/domains", handler.createDomain)

	missingZone := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewBufferString(`{"hostname":"app.example.com","originUrl":"http://localhost:8080"}`))
	missingZone.Header.Set("Content-Type", "application/json")
	missingResponse := httptest.NewRecorder()
	engine.ServeHTTP(missingResponse, missingZone)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("missing zone status = %d", missingResponse.Code)
	}

	valid := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewBufferString(`{"hostname":"app.example.com","originUrl":"http://localhost:8080","zoneId":"zone-1"}`))
	valid.Header.Set("Content-Type", "application/json")
	validResponse := httptest.NewRecorder()
	engine.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusCreated || !strings.Contains(validResponse.Body.String(), `"zoneId":"zone-1"`) {
		t.Fatalf("valid status/body = %d/%s", validResponse.Code, validResponse.Body.String())
	}
}

func TestGetAndListDomainsReturnZoneID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeRouteDomainService{domains: []*model.Domain{{ID: "domain-1", CloudflareZoneID: "zone-1"}}}
	engine := gin.New()
	handler := &DomainHandler{domainService: service}
	engine.GET("/api/domains/:id", handler.getDomain)
	engine.GET("/api/domains", handler.listDomains)

	for _, path := range []string{"/api/domains/domain-1", "/api/domains"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"zoneId":"zone-1"`) {
			t.Fatalf("%s status/body = %d/%s", path, response.Code, response.Body.String())
		}
	}
}
```

- [ ] **Step 2: Run route tests to verify missing handler**

Run: `go test ./internal/application/api/route/domain -run 'TestZoneList|TestListCloudflareZones|TestCreateDomainRequiresZoneID' -v`

Expected: FAIL because `listCloudflareZones` and `/api/cloudflare/zones` do not exist.

- [ ] **Step 3: Add zone-list handler**

Create `internal/application/api/route/domain/zones.go`:

```go
package domainroute

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) listCloudflareZones(c *gin.Context) {
	zones, err := h.domainService.ListCloudflareZones(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Cloudflare unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": zones})
}
```

- [ ] **Step 4: Register authenticated endpoint in existing route module**

Add to `DomainRoute.Setup` after domain route registration:

```go
cloudflare := r.Group("/api/cloudflare", middleware.JWTAuth(r.authService))
cloudflare.GET("/zones", r.domainHandler.listCloudflareZones)
```

Do not create a second service, route module, zone repository, or zone cache.

- [ ] **Step 5: Add create upstream mapping check**

Append to `route_test.go`:

```go
func TestCreateDomainMapsCloudflareFailureToBadGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := &DomainHandler{domainService: &fakeRouteDomainService{zonesErr: domainservice.ErrCloudflareUnavailable}}
	engine.POST("/api/domains", handler.createDomain)

	request := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewBufferString(`{"hostname":"app.example.com","originUrl":"http://localhost:8080","zoneId":"zone-1"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status/body = %d/%s", response.Code, response.Body.String())
	}
}
```

- [ ] **Step 6: Run route and full tests**

Run: `go test ./internal/application/api/route/domain -v`

Expected: PASS.

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 7: Commit HTTP API**

```bash
git add internal/application/api/route/domain/zones.go internal/application/api/route/domain/route.go internal/application/api/route/domain/route_test.go
git commit -m "feat: expose Cloudflare zones API"
```

---

### Task 5: Remove Global Zone Configuration and Document Rollout

**Files:**
- Modify: `internal/pkg/config/config.go`
- Create: `internal/pkg/config/config_test.go`
- Modify: `.env.example`
- Modify: `README.md`

**Interfaces:**
- Removes: `config.Config.CloudflareZoneID`.
- Removes runtime requirement: `CLOUDFLARE_ZONE_ID`.
- Keeps required: `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID`.

- [ ] **Step 1: Write config regression test**

Create `internal/pkg/config/config_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

func TestLoadDoesNotRequireCloudflareZoneID(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "token")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "account")
	t.Setenv("CLOUDFLARE_ZONE_ID", "")
	t.Setenv("ENCRYPTION_KEY", strings.Repeat("01", 32))
	t.Setenv("DB_PATH", "test.db")
	t.Setenv("LOG_DIR", t.TempDir())
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("METRICS_PORT_RANGE_START", "20500")
	t.Setenv("METRICS_PORT_RANGE_END", "20501")
	t.Setenv("CLOUDFLARED_BINARY", "cloudflared")
	t.Setenv("CLOUDFLARED_PROTOCOL", "http2")
	t.Setenv("ADMIN_USERNAME", "admin")
	t.Setenv("ADMIN_PASSWORD", "password")
	t.Setenv("JWT_SECRET", strings.Repeat("02", 32))
	t.Setenv("JWT_TTL", "1h")

	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run test to verify old requirement fails**

Run: `go test ./internal/pkg/config -run TestLoadDoesNotRequireCloudflareZoneID -v`

Expected: FAIL with `CLOUDFLARE_ZONE_ID is required`.

- [ ] **Step 3: Remove obsolete config field and validation**

Delete from `internal/pkg/config/config.go`:

```go
CloudflareZoneID string
```

Delete loader assignment:

```go
CloudflareZoneID: v.GetString("CLOUDFLARE_ZONE_ID"),
```

Delete validation:

```go
if cfg.CloudflareZoneID == "" {
	return Config{}, fmt.Errorf("CLOUDFLARE_ZONE_ID is required")
}
```

Delete this line from `.env.example`:

```env
CLOUDFLARE_ZONE_ID=
```

- [ ] **Step 4: Update operator documentation**

Change prerequisites in `README.md` to:

```markdown
- A Cloudflare API token with Tunnel write access on the target account, `Zone Read`, and `DNS Edit` on every selectable zone
- The Cloudflare account ID that owns the tunnels and zones
```

Remove `CLOUDFLARE_ZONE_ID` from environment variable table. Update token description to include zone listing and DNS operations across target zones.

Add endpoint row:

```markdown
| `GET` | `/api/cloudflare/zones` | List active Cloudflare zones available for domain creation. |
```

Add create request contract:

```json
{
  "hostname": "app.example.com",
  "originUrl": "http://app:8080",
  "zoneId": "023e105f4ecef8ad9ca31a8372d0c353"
}
```

Add deployment warning in normal prose:

1. Stop backend before migration.
2. Remove old remote tunnels and DNS records that should not survive.
3. Run `make migrate`; migration `00004` deletes every local domain row.
4. Start backend and call `GET /api/cloudflare/zones`.
5. Recreate domains with returned zone IDs.

State explicitly: Goose down is also destructive, and migration does not delete remote Cloudflare resources.

- [ ] **Step 5: Verify no runtime global-zone references remain**

Run: `grep -R "CLOUDFLARE_ZONE_ID\|CloudflareZoneID" -- .env.example README.md internal/pkg/config internal/pkg/cloudflare`

Expected: no output. `CloudflareZoneID` remains only in domain model/service/tests, where it represents persisted per-domain ownership.

Run: `go test ./internal/pkg/config -run TestLoadDoesNotRequireCloudflareZoneID -v`

Expected: PASS.

- [ ] **Step 6: Run complete verification**

Run: `gofmt -w internal/model/cloudflare.go internal/model/domain.go internal/pkg/request/domain/create.go migrations/migrations_test.go internal/pkg/cloudflare/type.go internal/pkg/cloudflare/client.go internal/pkg/cloudflare/client_test.go internal/services/domain/service.go internal/services/domain/zones.go internal/services/domain/create.go internal/services/domain/delete.go internal/services/domain/service_test.go internal/application/api/route/domain/create.go internal/application/api/route/domain/zones.go internal/application/api/route/domain/route.go internal/application/api/route/domain/route_test.go internal/pkg/config/config.go internal/pkg/config/config_test.go`

Expected: command exits 0.

Run: `go test ./...`

Expected: PASS for every package.

Run: `go build ./...`

Expected: PASS with no output.

Run: `git diff --check`

Expected: no output.

- [ ] **Step 7: Commit config and docs**

```bash
git add internal/pkg/config/config.go internal/pkg/config/config_test.go .env.example README.md
git commit -m "docs: document multi-zone rollout"
```

---

## Final Review Checklist

- [ ] Every Cloudflare zone-list request includes configured account ID and active status filter.
- [ ] Zone pagination reaches an empty page and propagates pager errors.
- [ ] Domain create rejects unknown, inactive, and hostname-mismatched zones before tunnel creation.
- [ ] DNS create and rollback use request `zoneId`.
- [ ] DNS delete uses persisted `domain.CloudflareZoneID`.
- [ ] `GET /api/cloudflare/zones` is JWT-protected and hides upstream error details.
- [ ] Domain create/get/list JSON includes `zoneId` through model serialization.
- [ ] `CLOUDFLARE_ZONE_ID` is absent from runtime config and docs.
- [ ] Migration and rollback destructiveness is documented.
- [ ] `go test ./...`, `go build ./...`, and `git diff --check` pass.
