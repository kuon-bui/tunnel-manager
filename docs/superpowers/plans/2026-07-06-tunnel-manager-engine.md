# Tunnel Manager Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go backend (`backend/`) that creates, supervises, and tears down one Cloudflare Tunnel per exposed domain, driven entirely through a REST API.

**Architecture:** A single Go binary spawns `cloudflared tunnel run --token <TOKEN>` as a supervised child process per domain, using Cloudflare's remote-managed tunnel configuration (no local `config.yml`/`credentials.json`). State lives in SQLite. The Cloudflare API (create tunnel, push ingress config, create/delete DNS record, delete tunnel) is called through a narrow interface so the orchestration logic can be unit-tested without live credentials.

**Tech Stack:** Go 1.23, Gin (HTTP), `github.com/uptrace/bun` + `modernc.org/sqlite` (pure-Go, no cgo), `github.com/cloudflare/cloudflare-go/v6`, `github.com/google/uuid`, standard library `os/exec` + `crypto/aes`.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-07-06-tunnel-manager-engine-design.md` — every requirement in that file must map to a task below.
- One Cloudflare Tunnel per domain; tunnel config is remote-managed (token-based), never a local `config.yml`.
- `tunnel_token` is AES-GCM encrypted at rest and never appears in any API response.
- Crash backoff: 1s, 2s, 4s, 8s, 16s (5 attempts), then `status=error`; restart counter resets after 60s of continuous uptime.
- Metrics port range default: `20500-20999`, loopback-only, proxied through the backend's own HTTP API — never published as container ports.
- Pinned dependency versions (confirmed compiling together via `go build` during planning — see Task 1): `github.com/cloudflare/cloudflare-go/v6 v6.10.0`, `github.com/gin-gonic/gin v1.12.0`, `github.com/uptrace/bun v1.2.18`, `github.com/uptrace/bun/dialect/sqlitedialect v1.2.18`, `modernc.org/sqlite v1.53.0`, `github.com/google/uuid v1.6.0`.
- **Spec refinement found during planning:** the spec's `domains` table (Data Model section) omits a column needed to fulfill its own "Delete domain" flow — deleting a Cloudflare DNS record requires the record's ID, not just the hostname. Task 3 adds `dns_record_id` to the table. This is called out here so it isn't mistaken for scope drift.
- **Spec refinement found during planning:** the spec's env var list only mentions `CLOUDFLARE_API_TOKEN` and `ENCRYPTION_KEY`, but the Cloudflare API requires an account ID and zone ID as separate values from the token. Task 1 adds `CLOUDFLARE_ACCOUNT_ID` and `CLOUDFLARE_ZONE_ID` as required env vars (still a single Cloudflare account for the whole system, consistent with the spec's intent).

---

## File Structure

```
backend/
  go.mod, go.sum
  cmd/server/main.go                     # wiring + graceful shutdown + boot reconciliation
  internal/config/config.go              # env var loading
  internal/config/config_test.go
  internal/crypto/crypto.go              # AES-GCM encrypt/decrypt
  internal/crypto/crypto_test.go
  internal/store/store.go                # bun model + repository (CRUD)
  internal/store/store_test.go
  internal/portalloc/portalloc.go        # metrics port allocator
  internal/portalloc/portalloc_test.go
  internal/logbuf/logbuf.go              # ring buffer + file-backed log writer
  internal/logbuf/logbuf_test.go
  internal/cfclient/cfclient.go          # Cloudflare API wrapper (interface + real impl)
  internal/cfclient/cfclient_test.go
  internal/supervisor/supervisor.go      # process supervisor (spawn/crash/backoff)
  internal/supervisor/supervisor_test.go
  internal/service/domainservice.go      # orchestration: create/update/delete/stop/restart/reconcile
  internal/service/domainservice_test.go
  internal/api/router.go                 # gin route registration
  internal/api/handlers.go               # HTTP handlers + response DTOs
  internal/api/handlers_test.go
  Dockerfile
```

---

### Task 1: Module scaffold & config loader

**Files:**
- Create: `backend/go.mod`, `backend/go.sum`
- Create: `backend/internal/config/config.go`
- Test: `backend/internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config` struct and `config.Load() (Config, error)`, consumed by `cmd/server/main.go` (Task 11) and indirectly by every other package that needs a config value passed in explicitly (no package other than `main` reads env vars directly).

- [ ] **Step 1: Scaffold the Go module**

```bash
mkdir -p backend/cmd/server backend/internal/config
cd backend
go mod init tunnelmanager
go get github.com/cloudflare/cloudflare-go/v6@v6.10.0
go get github.com/gin-gonic/gin@v1.12.0
go get github.com/uptrace/bun@v1.2.18
go get github.com/uptrace/bun/dialect/sqlitedialect@v1.2.18
go get modernc.org/sqlite@v1.53.0
go get github.com/google/uuid@v1.6.0
```

Expected: `go.mod` and `go.sum` are created with no errors.

- [ ] **Step 2: Write the failing test for config loading**

```go
// backend/internal/config/config_test.go
package config

import (
	"testing"
)

func TestLoad_MissingRequiredVar(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	t.Setenv("CLOUDFLARE_ZONE_ID", "zone")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when CLOUDFLARE_API_TOKEN is missing, got nil")
	}
}

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "token")
	t.Setenv("CLOUDFLARE_ACCOUNT_ID", "acct")
	t.Setenv("CLOUDFLARE_ZONE_ID", "zone")
	t.Setenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	t.Setenv("DB_PATH", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("METRICS_PORT_RANGE_START", "")
	t.Setenv("METRICS_PORT_RANGE_END", "")
	t.Setenv("CLOUDFLARED_BINARY", "")
	t.Setenv("LOG_DIR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.DBPath != "/data/tunnel-manager.db" {
		t.Errorf("DBPath default = %q, want /data/tunnel-manager.db", cfg.DBPath)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr default = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.MetricsPortRangeStart != 20500 || cfg.MetricsPortRangeEnd != 20999 {
		t.Errorf("metrics port range = %d-%d, want 20500-20999", cfg.MetricsPortRangeStart, cfg.MetricsPortRangeEnd)
	}
	if cfg.CloudflaredBinary != "cloudflared" {
		t.Errorf("CloudflaredBinary default = %q, want cloudflared", cfg.CloudflaredBinary)
	}
	if len(cfg.EncryptionKey) != 32 {
		t.Errorf("EncryptionKey len = %d, want 32", len(cfg.EncryptionKey))
	}
}
```

- [ ] **Step 2b: Run test to verify it fails**

Run: `cd backend && go test ./internal/config/... -v`
Expected: FAIL — `config.Load` undefined.

- [ ] **Step 3: Implement the config loader**

```go
// backend/internal/config/config.go
package config

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	CloudflareAPIToken   string
	CloudflareAccountID  string
	CloudflareZoneID     string
	EncryptionKey        []byte
	DBPath               string
	LogDir               string
	HTTPAddr             string
	MetricsPortRangeStart int
	MetricsPortRangeEnd   int
	CloudflaredBinary    string
}

func Load() (Config, error) {
	cfg := Config{
		CloudflareAPIToken:  os.Getenv("CLOUDFLARE_API_TOKEN"),
		CloudflareAccountID: os.Getenv("CLOUDFLARE_ACCOUNT_ID"),
		CloudflareZoneID:    os.Getenv("CLOUDFLARE_ZONE_ID"),
		DBPath:              envOrDefault("DB_PATH", "/data/tunnel-manager.db"),
		LogDir:              envOrDefault("LOG_DIR", "/data/logs"),
		HTTPAddr:            envOrDefault("HTTP_ADDR", ":8080"),
		CloudflaredBinary:   envOrDefault("CLOUDFLARED_BINARY", "cloudflared"),
	}

	if cfg.CloudflareAPIToken == "" {
		return Config{}, fmt.Errorf("CLOUDFLARE_API_TOKEN is required")
	}
	if cfg.CloudflareAccountID == "" {
		return Config{}, fmt.Errorf("CLOUDFLARE_ACCOUNT_ID is required")
	}
	if cfg.CloudflareZoneID == "" {
		return Config{}, fmt.Errorf("CLOUDFLARE_ZONE_ID is required")
	}

	keyHex := os.Getenv("ENCRYPTION_KEY")
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return Config{}, fmt.Errorf("ENCRYPTION_KEY must be hex-encoded: %w", err)
	}
	if len(key) != 32 {
		return Config{}, fmt.Errorf("ENCRYPTION_KEY must decode to 32 bytes (got %d)", len(key))
	}
	cfg.EncryptionKey = key

	start, err := envIntOrDefault("METRICS_PORT_RANGE_START", 20500)
	if err != nil {
		return Config{}, err
	}
	end, err := envIntOrDefault("METRICS_PORT_RANGE_END", 20999)
	if err != nil {
		return Config{}, err
	}
	if end <= start {
		return Config{}, fmt.Errorf("METRICS_PORT_RANGE_END (%d) must be greater than METRICS_PORT_RANGE_START (%d)", end, start)
	}
	cfg.MetricsPortRangeStart = start
	cfg.MetricsPortRangeEnd = end

	return cfg, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOrDefault(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return n, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/config/... -v`
Expected: PASS (both `TestLoad_MissingRequiredVar` and `TestLoad_Defaults`)

- [ ] **Step 5: Commit**

```bash
git add backend/go.mod backend/go.sum backend/internal/config
git commit -m "feat(backend): scaffold Go module and config loader"
```

---

### Task 2: Crypto — AES-GCM encrypt/decrypt for the tunnel token

**Files:**
- Create: `backend/internal/crypto/crypto.go`
- Test: `backend/internal/crypto/crypto_test.go`

**Interfaces:**
- Produces: `crypto.Encrypt(key []byte, plaintext string) (string, error)`, `crypto.Decrypt(key []byte, ciphertext string) (string, error)` — both consumed by Task 8 (`domainservice`) to protect `tunnel_token` before it reaches `store.Domain`.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/crypto/crypto_test.go
package crypto

import "testing"

var testKey = []byte("01234567890123456789012345678901"[:32])

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	plaintext := "super-secret-tunnel-token"

	ciphertext, err := Encrypt(testKey, plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ciphertext == plaintext {
		t.Fatal("ciphertext must not equal plaintext")
	}

	got, err := Decrypt(testKey, ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plaintext {
		t.Errorf("Decrypt() = %q, want %q", got, plaintext)
	}
}

func TestEncrypt_DifferentCiphertextEachTime(t *testing.T) {
	a, _ := Encrypt(testKey, "same-input")
	b, _ := Encrypt(testKey, "same-input")
	if a == b {
		t.Error("two encryptions of the same plaintext must differ (random nonce)")
	}
}

func TestDecrypt_WrongKeyFails(t *testing.T) {
	ciphertext, _ := Encrypt(testKey, "secret")
	wrongKey := []byte("99999999999999999999999999999999"[:32])
	if _, err := Decrypt(wrongKey, ciphertext); err == nil {
		t.Error("expected error decrypting with wrong key, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/crypto/... -v`
Expected: FAIL — `Encrypt`/`Decrypt` undefined.

- [ ] **Step 3: Implement AES-GCM encrypt/decrypt**

```go
// backend/internal/crypto/crypto.go
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// Encrypt returns base64(nonce || ciphertext). key must be 32 bytes (AES-256).
func Encrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: read nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func Decrypt(key []byte, ciphertext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("crypto: decode base64: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("crypto: ciphertext too short")
	}
	nonce, sealed := raw[:nonceSize], raw[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, sealed, nil)
	if err != nil {
		return "", fmt.Errorf("crypto: decrypt: %w", err)
	}
	return string(plaintext), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/crypto/... -v`
Expected: PASS (all three tests)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/crypto
git commit -m "feat(backend): add AES-GCM encrypt/decrypt for tunnel tokens"
```

---

### Task 3: SQLite store — model + repository

**Files:**
- Create: `backend/internal/store/store.go`
- Test: `backend/internal/store/store_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `store.Status` (type + constants `StatusPending`, `StatusActive`, `StatusError`, `StatusStopped`), `store.Domain` struct, `store.NewRepository(db *bun.DB) *Repository`, and repository methods:
  `Migrate(ctx) error`, `Create(ctx, *Domain) error`, `List(ctx) ([]Domain, error)`, `Get(ctx, id string) (*Domain, error)`, `GetByHostname(ctx, hostname string) (*Domain, error)`, `ListByStatus(ctx, status Status) ([]Domain, error)`, `Update(ctx, *Domain) error`, `Delete(ctx, id string) error`.
  These are consumed by Task 8 (`domainservice`).

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/store/store_test.go
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"
)

func newTestRepo(t *testing.T) *Repository {
	t.Helper()
	// A named (not anonymous) shared-cache memory DB: multiple pooled
	// connections within this test see the same schema/data, but the name
	// (derived from the test name) keeps it isolated from every other test
	// running in the same process. An anonymous "file::memory:?cache=shared"
	// DSN is shared process-wide and leaks rows between tests.
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqldb.SetMaxIdleConns(10)
	sqldb.SetConnMaxLifetime(0)
	db := bun.NewDB(sqldb, sqlitedialect.New())
	repo := NewRepository(db)
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return repo
}

func TestRepository_CreateAndGet(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	d := &Domain{
		ID:                   "d1",
		Hostname:             "n8n.example.com",
		OriginURL:            "http://n8n:5678",
		CloudflareTunnelID:   "tunnel-1",
		DNSRecordID:          "dns-1",
		EncryptedTunnelToken: "enc-token",
		Status:               StatusPending,
		MetricsPort:          20500,
		RestartCount:         0,
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
	if err := repo.Create(ctx, d); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.Get(ctx, "d1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Hostname != "n8n.example.com" {
		t.Errorf("Hostname = %q, want n8n.example.com", got.Hostname)
	}

	byHost, err := repo.GetByHostname(ctx, "n8n.example.com")
	if err != nil {
		t.Fatalf("GetByHostname: %v", err)
	}
	if byHost.ID != "d1" {
		t.Errorf("GetByHostname ID = %q, want d1", byHost.ID)
	}
}

func TestRepository_UpdateAndListByStatus(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	d := &Domain{
		ID: "d2", Hostname: "openclaw.example.com", OriginURL: "http://openclaw:18789",
		CloudflareTunnelID: "tunnel-2", DNSRecordID: "dns-2", EncryptedTunnelToken: "enc",
		Status: StatusPending, MetricsPort: 20501, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := repo.Create(ctx, d); err != nil {
		t.Fatalf("Create: %v", err)
	}

	d.Status = StatusActive
	d.PID = 1234
	if err := repo.Update(ctx, d); err != nil {
		t.Fatalf("Update: %v", err)
	}

	active, err := repo.ListByStatus(ctx, StatusActive)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(active) != 1 || active[0].PID != 1234 {
		t.Errorf("ListByStatus(active) = %+v, want one record with PID 1234", active)
	}

	pending, err := repo.ListByStatus(ctx, StatusPending)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("ListByStatus(pending) = %+v, want empty", pending)
	}
}

func TestRepository_Delete(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	d := &Domain{
		ID: "d3", Hostname: "x.example.com", OriginURL: "http://x:80",
		CloudflareTunnelID: "t3", DNSRecordID: "dns-3", EncryptedTunnelToken: "enc",
		Status: StatusPending, MetricsPort: 20502, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if err := repo.Create(ctx, d); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, "d3"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, "d3"); err == nil {
		t.Error("expected error getting deleted domain, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/... -v`
Expected: FAIL — `store.NewRepository`, `store.Domain`, etc. undefined.

- [ ] **Step 3: Implement the model and repository**

```go
// backend/internal/store/store.go
package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"
)

type Status string

const (
	StatusPending Status = "pending"
	StatusActive  Status = "active"
	StatusError   Status = "error"
	StatusStopped Status = "stopped"
)

type Domain struct {
	bun.BaseModel `bun:"table:domains,alias:d"`

	ID                   string    `bun:"id,pk"`
	Hostname             string    `bun:"hostname,notnull,unique"`
	OriginURL            string    `bun:"origin_url,notnull"`
	CloudflareTunnelID   string    `bun:"cloudflare_tunnel_id,notnull"`
	DNSRecordID          string    `bun:"dns_record_id,notnull"`
	EncryptedTunnelToken string    `bun:"tunnel_token,notnull"`
	Status               Status    `bun:"status,notnull"`
	MetricsPort          int       `bun:"metrics_port,notnull"`
	PID                  int       `bun:"pid"`
	RestartCount         int       `bun:"restart_count,notnull,default:0"`
	LastError            string    `bun:"last_error"`
	CreatedAt            time.Time `bun:"created_at,notnull"`
	UpdatedAt            time.Time `bun:"updated_at,notnull"`
}

type Repository struct {
	db *bun.DB
}

func NewRepository(db *bun.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Migrate(ctx context.Context) error {
	_, err := r.db.NewCreateTable().Model((*Domain)(nil)).IfNotExists().Exec(ctx)
	return err
}

func (r *Repository) Create(ctx context.Context, d *Domain) error {
	_, err := r.db.NewInsert().Model(d).Exec(ctx)
	return err
}

func (r *Repository) List(ctx context.Context) ([]Domain, error) {
	var domains []Domain
	err := r.db.NewSelect().Model(&domains).Order("created_at ASC").Scan(ctx)
	return domains, err
}

func (r *Repository) Get(ctx context.Context, id string) (*Domain, error) {
	d := new(Domain)
	err := r.db.NewSelect().Model(d).Where("id = ?", id).Scan(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (r *Repository) GetByHostname(ctx context.Context, hostname string) (*Domain, error) {
	d := new(Domain)
	err := r.db.NewSelect().Model(d).Where("hostname = ?", hostname).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (r *Repository) ListByStatus(ctx context.Context, status Status) ([]Domain, error) {
	var domains []Domain
	err := r.db.NewSelect().Model(&domains).Where("status = ?", status).Order("created_at ASC").Scan(ctx)
	return domains, err
}

func (r *Repository) Update(ctx context.Context, d *Domain) error {
	d.UpdatedAt = time.Now().UTC()
	_, err := r.db.NewUpdate().Model(d).WherePK().Exec(ctx)
	return err
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*Domain)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/... -v`
Expected: PASS (all subtests)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store
git commit -m "feat(backend): add SQLite domain model and repository"
```

---

### Task 4: Metrics port allocator

**Files:**
- Create: `backend/internal/portalloc/portalloc.go`
- Test: `backend/internal/portalloc/portalloc_test.go`

**Interfaces:**
- Produces: `portalloc.NewAllocator(start, end int) *Allocator`, `(*Allocator) Allocate(taken map[int]bool) (int, error)` — consumed by Task 8 (`domainservice`), which supplies `taken` from `repo.List()`.

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/portalloc/portalloc_test.go
package portalloc

import "testing"

func TestAllocate_ReturnsFirstFree(t *testing.T) {
	a := NewAllocator(20500, 20502)
	port, err := a.Allocate(map[int]bool{20500: true})
	if err != nil {
		t.Fatalf("Allocate: %v", err)
	}
	if port != 20501 {
		t.Errorf("port = %d, want 20501", port)
	}
}

func TestAllocate_AllTakenReturnsError(t *testing.T) {
	a := NewAllocator(20500, 20501)
	_, err := a.Allocate(map[int]bool{20500: true, 20501: true})
	if err == nil {
		t.Fatal("expected error when all ports are taken, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/portalloc/... -v`
Expected: FAIL — `portalloc.NewAllocator` undefined.

- [ ] **Step 3: Implement the allocator**

```go
// backend/internal/portalloc/portalloc.go
package portalloc

import "fmt"

type Allocator struct {
	start, end int
}

func NewAllocator(start, end int) *Allocator {
	return &Allocator{start: start, end: end}
}

func (a *Allocator) Allocate(taken map[int]bool) (int, error) {
	for port := a.start; port <= a.end; port++ {
		if !taken[port] {
			return port, nil
		}
	}
	return 0, fmt.Errorf("portalloc: no free port in range %d-%d", a.start, a.end)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/portalloc/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/portalloc
git commit -m "feat(backend): add metrics port allocator"
```

---

### Task 5: Log capture — ring buffer + file writer

**Files:**
- Create: `backend/internal/logbuf/logbuf.go`
- Test: `backend/internal/logbuf/logbuf_test.go`

**Interfaces:**
- Produces: `logbuf.NewBuffer(filePath string, capacity int) (*Buffer, error)`, `(*Buffer) Write(p []byte) (int, error)` (implements `io.Writer`), `(*Buffer) Lines() []string`, `(*Buffer) Close() error`. Consumed by Task 7 (`supervisor`, as the subprocess's `Stdout`/`Stderr`) and Task 9 (`api`, to serve `GET /api/domains/{id}/logs`).

- [ ] **Step 1: Write the failing test**

```go
// backend/internal/logbuf/logbuf_test.go
package logbuf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuffer_WriteAndLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	buf, err := NewBuffer(path, 3)
	if err != nil {
		t.Fatalf("NewBuffer: %v", err)
	}
	defer buf.Close()

	buf.Write([]byte("line1\n"))
	buf.Write([]byte("line2\n"))
	buf.Write([]byte("line3\n"))
	buf.Write([]byte("line4\n"))

	lines := buf.Lines()
	if len(lines) != 3 {
		t.Fatalf("len(lines) = %d, want 3", len(lines))
	}
	if lines[0] != "line2" || lines[2] != "line4" {
		t.Errorf("lines = %v, want [line2 line3 line4]", lines)
	}

	buf.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	want := "line1\nline2\nline3\nline4\n"
	if string(data) != want {
		t.Errorf("file contents = %q, want %q", string(data), want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/logbuf/... -v`
Expected: FAIL — `logbuf.NewBuffer` undefined.

- [ ] **Step 3: Implement the ring buffer + file writer**

```go
// backend/internal/logbuf/logbuf.go
package logbuf

import (
	"bufio"
	"os"
	"strings"
	"sync"
)

// Buffer is an io.Writer that keeps the last `capacity` newline-delimited
// lines in memory while also appending every byte written to a file on disk.
type Buffer struct {
	mu       sync.Mutex
	capacity int
	lines    []string
	partial  string
	file     *os.File
	writer   *bufio.Writer
}

func NewBuffer(filePath string, capacity int) (*Buffer, error) {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Buffer{
		capacity: capacity,
		file:     f,
		writer:   bufio.NewWriter(f),
	}, nil
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, err := b.writer.Write(p); err != nil {
		return 0, err
	}
	if err := b.writer.Flush(); err != nil {
		return 0, err
	}

	b.partial += string(p)
	for {
		idx := strings.IndexByte(b.partial, '\n')
		if idx < 0 {
			break
		}
		line := b.partial[:idx]
		b.partial = b.partial[idx+1:]
		b.lines = append(b.lines, line)
		if len(b.lines) > b.capacity {
			b.lines = b.lines[len(b.lines)-b.capacity:]
		}
	}
	return len(p), nil
}

func (b *Buffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

func (b *Buffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writer.Flush(); err != nil {
		return err
	}
	return b.file.Close()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/logbuf/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/logbuf
git commit -m "feat(backend): add ring-buffer log capture with file persistence"
```

---

### Task 6: Cloudflare API client wrapper

**Files:**
- Create: `backend/internal/cfclient/cfclient.go`
- Test: `backend/internal/cfclient/cfclient_test.go`

**Interfaces:**
- Produces: `cfclient.TunnelInfo{TunnelID, Token string}`, the `cfclient.Client` interface (`CreateTunnel`, `PutIngressConfig`, `CreateDNSRecord`, `DeleteDNSRecord`, `DeleteTunnel`), and `cfclient.New(apiToken, accountID, zoneID string) Client`. Consumed by Task 8 (`domainservice`), which depends only on the `Client` interface (a hand-written fake implementing it is used in that task's tests — no mocking library needed).

> **Note on verification:** the exact Cloudflare SDK call shapes below were confirmed by pinning `github.com/cloudflare/cloudflare-go/v6 v6.10.0` in a scratch module and running `go doc` against the installed package, then compiling a probe file against them. `go doc github.com/cloudflare/cloudflare-go/v6/zero_trust TunnelCloudflaredNewParams` (and the sibling types) is the command to re-verify if a future dependency bump changes field names.

- [ ] **Step 1: Write the failing test**

This test only checks that `New` returns a non-nil `Client` — the real Cloudflare API cannot be exercised in an automated test without live credentials. Full behavioral coverage of the orchestration that calls this client (including rollback on failure) is in Task 8, against a hand-written fake.

```go
// backend/internal/cfclient/cfclient_test.go
package cfclient

import "testing"

func TestNew_ReturnsClient(t *testing.T) {
	c := New("dummy-token", "dummy-account", "dummy-zone")
	if c == nil {
		t.Fatal("New returned nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/cfclient/... -v`
Expected: FAIL — `cfclient.New` undefined.

- [ ] **Step 3: Implement the Cloudflare client wrapper**

```go
// backend/internal/cfclient/cfclient.go
package cfclient

import (
	"context"
	"fmt"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	"github.com/cloudflare/cloudflare-go/v6/option"
	"github.com/cloudflare/cloudflare-go/v6/zero_trust"
)

type TunnelInfo struct {
	TunnelID string
	Token    string
}

// Client is the narrow surface the rest of this backend needs from
// Cloudflare. Keeping it an interface lets domainservice tests use a
// hand-written fake instead of hitting the real API.
type Client interface {
	CreateTunnel(ctx context.Context, name string) (TunnelInfo, error)
	PutIngressConfig(ctx context.Context, tunnelID, hostname, originURL string) error
	CreateDNSRecord(ctx context.Context, hostname, tunnelID string) (dnsRecordID string, err error)
	DeleteDNSRecord(ctx context.Context, dnsRecordID string) error
	DeleteTunnel(ctx context.Context, tunnelID string) error
}

type client struct {
	api       *cloudflare.Client
	accountID string
	zoneID    string
}

func New(apiToken, accountID, zoneID string) Client {
	return &client{
		api:       cloudflare.NewClient(option.WithAPIToken(apiToken)),
		accountID: accountID,
		zoneID:    zoneID,
	}
}

func (c *client) CreateTunnel(ctx context.Context, name string) (TunnelInfo, error) {
	tunnel, err := c.api.ZeroTrust.Tunnels.Cloudflared.New(ctx, zero_trust.TunnelCloudflaredNewParams{
		AccountID: cloudflare.F(c.accountID),
		Name:      cloudflare.F(name),
		ConfigSrc: cloudflare.F(zero_trust.TunnelCloudflaredNewParamsConfigSrcCloudflare),
	})
	if err != nil {
		return TunnelInfo{}, fmt.Errorf("cfclient: create tunnel: %w", err)
	}

	token, err := c.api.ZeroTrust.Tunnels.Cloudflared.Token.Get(ctx, tunnel.ID, zero_trust.TunnelCloudflaredTokenGetParams{
		AccountID: cloudflare.F(c.accountID),
	})
	if err != nil {
		return TunnelInfo{}, fmt.Errorf("cfclient: get tunnel token: %w", err)
	}

	return TunnelInfo{TunnelID: tunnel.ID, Token: *token}, nil
}

func (c *client) PutIngressConfig(ctx context.Context, tunnelID, hostname, originURL string) error {
	_, err := c.api.ZeroTrust.Tunnels.Cloudflared.Configurations.Update(ctx, tunnelID, zero_trust.TunnelCloudflaredConfigurationUpdateParams{
		AccountID: cloudflare.F(c.accountID),
		Config: cloudflare.F(zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfig{
			Ingress: cloudflare.F([]zero_trust.TunnelCloudflaredConfigurationUpdateParamsConfigIngress{
				{Hostname: cloudflare.F(hostname), Service: cloudflare.F(originURL)},
				{Service: cloudflare.F("http_status:404")},
			}),
		}),
	})
	if err != nil {
		return fmt.Errorf("cfclient: put ingress config: %w", err)
	}
	return nil
}

func (c *client) CreateDNSRecord(ctx context.Context, hostname, tunnelID string) (string, error) {
	rec, err := c.api.DNS.Records.New(ctx, dns.RecordNewParams{
		ZoneID: cloudflare.F(c.zoneID),
		Body: dns.CNAMERecordParam{
			Name:    cloudflare.F(hostname),
			Type:    cloudflare.F(dns.CNAMERecordTypeCNAME),
			Content: cloudflare.F(tunnelID + ".cfargotunnel.com"),
			TTL:     cloudflare.F(dns.TTL(1)),
			Proxied: cloudflare.F(true),
		},
	})
	if err != nil {
		return "", fmt.Errorf("cfclient: create dns record: %w", err)
	}
	return rec.ID, nil
}

func (c *client) DeleteDNSRecord(ctx context.Context, dnsRecordID string) error {
	_, err := c.api.DNS.Records.Delete(ctx, dnsRecordID, dns.RecordDeleteParams{
		ZoneID: cloudflare.F(c.zoneID),
	})
	if err != nil {
		return fmt.Errorf("cfclient: delete dns record: %w", err)
	}
	return nil
}

func (c *client) DeleteTunnel(ctx context.Context, tunnelID string) error {
	_, err := c.api.ZeroTrust.Tunnels.Cloudflared.Delete(ctx, tunnelID, zero_trust.TunnelCloudflaredDeleteParams{
		AccountID: cloudflare.F(c.accountID),
	})
	if err != nil {
		return fmt.Errorf("cfclient: delete tunnel: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/cfclient/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/cfclient
git commit -m "feat(backend): add Cloudflare Tunnel API client wrapper"
```

---

### Task 7: Process supervisor

**Files:**
- Create: `backend/internal/supervisor/supervisor.go`
- Test: `backend/internal/supervisor/supervisor_test.go`

**Interfaces:**
- Consumes: `io.Writer` (satisfied by `logbuf.Buffer` from Task 5) as the subprocess's combined stdout/stderr sink.
- Produces: `supervisor.Event{DomainID string, PID int, Status store.Status, Err error}`, `supervisor.New(binary string, onEvent func(Event)) *Supervisor`, `(*Supervisor) Start(domainID, token string, metricsPort int, logWriter io.Writer) error`, `(*Supervisor) Stop(domainID string) error`, `(*Supervisor) IsRunning(domainID string) bool`. Consumed by Task 8 (`domainservice`), which passes an `onEvent` callback that updates the repository.
- Two fields are exported for test injection only: `Supervisor.SleepFunc func(time.Duration)` (defaults to `time.Sleep`) and `Supervisor.StableAfter time.Duration` (defaults to 60s) — tests override both to run the backoff/reset state machine in milliseconds instead of real time.

- [ ] **Step 1: Write the failing test**

This test spawns `/bin/sh` scripts instead of the real `cloudflared` binary, since the supervisor only cares about process lifecycle, not what the process does.

```go
// backend/internal/supervisor/supervisor_test.go
package supervisor

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"tunnelmanager/internal/store"
)

// writeScript creates an executable shell script and returns its path.
func writeScript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-cloudflared.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

type eventRecorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *eventRecorder) record(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *eventRecorder) snapshot() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}

func waitForEvent(t *testing.T, rec *eventRecorder, status store.Status, timeout time.Duration) Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, e := range rec.snapshot() {
			if e.Status == status {
				return e
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status %q, got events: %+v", status, rec.snapshot())
	return Event{}
}

func TestSupervisor_LongRunningProcessStaysActive(t *testing.T) {
	script := writeScript(t, "sleep 5\n")
	rec := &eventRecorder{}
	sup := New(script, rec.record)

	devnull, _ := os.Open(os.DevNull)
	defer devnull.Close()

	if err := sup.Start("dom-1", "fake-token", 20500, devnull); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !sup.IsRunning("dom-1") {
		t.Error("expected IsRunning(dom-1) to be true right after Start")
	}
	if err := sup.Stop("dom-1"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitForEvent(t, rec, store.StatusStopped, 2*time.Second)
}

func TestSupervisor_CrashLoopEndsInError(t *testing.T) {
	script := writeScript(t, "exit 1\n")
	rec := &eventRecorder{}
	sup := New(script, rec.record)
	sup.SleepFunc = func(time.Duration) {} // skip real backoff delays in the test

	devnull, _ := os.Open(os.DevNull)
	defer devnull.Close()

	if err := sup.Start("dom-2", "fake-token", 20501, devnull); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ev := waitForEvent(t, rec, store.StatusError, 2*time.Second)
	if ev.DomainID != "dom-2" {
		t.Errorf("event domain = %q, want dom-2", ev.DomainID)
	}
}

func waitForEventCount(t *testing.T, rec *eventRecorder, n int, timeout time.Duration) []Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if snap := rec.snapshot(); len(snap) >= n {
			return snap
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d events, got: %+v", n, rec.snapshot())
	return nil
}

// The process deliberately outlives StableAfter (150ms > 50ms) before each
// crash, so the stable-reset timer fires before every crash. If reset works,
// restartCount never escalates past 1 across repeated crash/respawn cycles.
// If reset were broken, the second crash would report RestartCount 2, the
// third 3, and so on — this is what distinguishes the test from one that
// just waits for status=error, which (correctly) never happens here.
func TestSupervisor_StableRunResetsRestartCount(t *testing.T) {
	script := writeScript(t, "sleep 0.15 && exit 1\n")
	rec := &eventRecorder{}
	sup := New(script, rec.record)
	sup.SleepFunc = func(time.Duration) {}
	sup.StableAfter = 50 * time.Millisecond

	devnull, _ := os.Open(os.DevNull)
	defer devnull.Close()

	if err := sup.Start("dom-3", "fake-token", 20502, devnull); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// events[0] is the initial Start() (RestartCount 0). events[1] and
	// events[2] are the first two crash-triggered respawns.
	events := waitForEventCount(t, rec, 3, 3*time.Second)
	if events[1].RestartCount != 1 {
		t.Errorf("first respawn RestartCount = %d, want 1", events[1].RestartCount)
	}
	if events[2].RestartCount != 1 {
		t.Errorf("second respawn RestartCount = %d, want 1 (stable reset should prevent escalation to 2)", events[2].RestartCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/supervisor/... -v`
Expected: FAIL — `supervisor.New` undefined.

- [ ] **Step 3: Implement the supervisor**

```go
// backend/internal/supervisor/supervisor.go
package supervisor

import (
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"tunnelmanager/internal/store"
)

const maxRestartAttempts = 5

type Event struct {
	DomainID     string
	PID          int
	Status       store.Status
	RestartCount int
	Err          error
}

type managedProcess struct {
	mu            sync.Mutex
	cmd           *exec.Cmd
	stopRequested bool
	restartCount  int
	stableTimer   *time.Timer
}

type Supervisor struct {
	binary  string
	onEvent func(Event)

	// Overridable for tests only.
	SleepFunc   func(time.Duration)
	StableAfter time.Duration

	mu    sync.Mutex
	procs map[string]*managedProcess
}

func New(binary string, onEvent func(Event)) *Supervisor {
	return &Supervisor{
		binary:      binary,
		onEvent:     onEvent,
		SleepFunc:   time.Sleep,
		StableAfter: 60 * time.Second,
		procs:       make(map[string]*managedProcess),
	}
}

func (s *Supervisor) buildCmd(token string, metricsPort int, logWriter io.Writer) *exec.Cmd {
	cmd := exec.Command(s.binary, "tunnel", "run", "--token", token, "--metrics", fmt.Sprintf("localhost:%d", metricsPort))
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	return cmd
}

func (s *Supervisor) Start(domainID, token string, metricsPort int, logWriter io.Writer) error {
	cmd := s.buildCmd(token, metricsPort, logWriter)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("supervisor: start %s: %w", domainID, err)
	}

	mp := &managedProcess{cmd: cmd}
	s.mu.Lock()
	s.procs[domainID] = mp
	s.mu.Unlock()

	s.armStableTimer(mp)
	s.onEvent(Event{DomainID: domainID, PID: cmd.Process.Pid, Status: store.StatusActive})

	go s.supervise(domainID, token, metricsPort, logWriter, mp)
	return nil
}

func (s *Supervisor) armStableTimer(mp *managedProcess) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if mp.stableTimer != nil {
		mp.stableTimer.Stop()
	}
	mp.stableTimer = time.AfterFunc(s.StableAfter, func() {
		mp.mu.Lock()
		mp.restartCount = 0
		mp.mu.Unlock()
	})
}

func (s *Supervisor) supervise(domainID, token string, metricsPort int, logWriter io.Writer, mp *managedProcess) {
	for {
		waitErr := mp.cmd.Wait()

		mp.mu.Lock()
		if mp.stableTimer != nil {
			mp.stableTimer.Stop()
		}
		stopRequested := mp.stopRequested
		mp.mu.Unlock()

		if stopRequested {
			s.mu.Lock()
			delete(s.procs, domainID)
			s.mu.Unlock()
			s.onEvent(Event{DomainID: domainID, Status: store.StatusStopped})
			return
		}

		mp.mu.Lock()
		mp.restartCount++
		attempt := mp.restartCount
		mp.mu.Unlock()

		if attempt > maxRestartAttempts {
			s.mu.Lock()
			delete(s.procs, domainID)
			s.mu.Unlock()
			s.onEvent(Event{DomainID: domainID, Status: store.StatusError, RestartCount: maxRestartAttempts, Err: waitErr})
			return
		}

		backoff := time.Duration(1<<(attempt-1)) * time.Second
		s.SleepFunc(backoff)

		cmd := s.buildCmd(token, metricsPort, logWriter)
		if err := cmd.Start(); err != nil {
			// Count this as a failed attempt too; loop back and try again
			// (or hit maxRestartAttempts and surface status=error).
			continue
		}

		mp.mu.Lock()
		mp.cmd = cmd
		mp.mu.Unlock()
		s.armStableTimer(mp)
		s.onEvent(Event{DomainID: domainID, PID: cmd.Process.Pid, Status: store.StatusActive, RestartCount: attempt})
	}
}

func (s *Supervisor) Stop(domainID string) error {
	s.mu.Lock()
	mp, ok := s.procs[domainID]
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("supervisor: no running process for %s", domainID)
	}

	mp.mu.Lock()
	mp.stopRequested = true
	proc := mp.cmd.Process
	mp.mu.Unlock()

	return proc.Signal(syscall.SIGTERM)
}

func (s *Supervisor) IsRunning(domainID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.procs[domainID]
	return ok
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/supervisor/... -v -timeout 30s`
Expected: PASS (all three tests)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/supervisor
git commit -m "feat(backend): add cloudflared process supervisor with crash backoff"
```

---

### Task 8: Domain service — orchestration, rollback, reconciliation

**Files:**
- Create: `backend/internal/service/domainservice.go`
- Test: `backend/internal/service/domainservice_test.go`

**Interfaces:**
- Consumes: `store.Repository` (Task 3), `cfclient.Client` interface (Task 6), `supervisor.Supervisor` (Task 7), `portalloc.Allocator` (Task 4), `crypto.Encrypt`/`Decrypt` (Task 2), `logbuf.NewBuffer` (Task 5).
- Produces: `service.New(repo *store.Repository, cf cfclient.Client, sup *supervisor.Supervisor, ports *portalloc.Allocator, encKey []byte, logDir string) *Service` and methods `CreateDomain`, `ListDomains`, `GetDomain`, `UpdateOrigin`, `DeleteDomain`, `StopDomain`, `RestartDomain`, `Reconcile`, `HandleSupervisorEvent(ev supervisor.Event)`. Consumed by Task 9 (`api`) and Task 11 (`main.go`, which wires `HandleSupervisorEvent` as the supervisor's `onEvent` callback and calls `Reconcile` at boot).

- [ ] **Step 1: Write the failing tests**

A hand-written fake implements `cfclient.Client` so rollback behavior is fully controllable without a real Cloudflare account.

```go
// backend/internal/service/domainservice_test.go
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"

	"tunnelmanager/internal/cfclient"
	"tunnelmanager/internal/portalloc"
	"tunnelmanager/internal/store"
	"tunnelmanager/internal/supervisor"
)

type fakeCF struct {
	createTunnelErr   error
	putIngressErr     error
	createDNSErr      error
	deletedTunnelIDs  []string
	deletedDNSIDs     []string
	nextTunnelID      string
	nextDNSRecordID   string
}

func (f *fakeCF) CreateTunnel(ctx context.Context, name string) (cfclient.TunnelInfo, error) {
	if f.createTunnelErr != nil {
		return cfclient.TunnelInfo{}, f.createTunnelErr
	}
	return cfclient.TunnelInfo{TunnelID: f.nextTunnelID, Token: "fake-token"}, nil
}

func (f *fakeCF) PutIngressConfig(ctx context.Context, tunnelID, hostname, originURL string) error {
	return f.putIngressErr
}

func (f *fakeCF) CreateDNSRecord(ctx context.Context, hostname, tunnelID string) (string, error) {
	if f.createDNSErr != nil {
		return "", f.createDNSErr
	}
	return f.nextDNSRecordID, nil
}

func (f *fakeCF) DeleteDNSRecord(ctx context.Context, dnsRecordID string) error {
	f.deletedDNSIDs = append(f.deletedDNSIDs, dnsRecordID)
	return nil
}

func (f *fakeCF) DeleteTunnel(ctx context.Context, tunnelID string) error {
	f.deletedTunnelIDs = append(f.deletedTunnelIDs, tunnelID)
	return nil
}

func newTestService(t *testing.T, cf cfclient.Client) (*Service, *store.Repository) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqldb.SetMaxIdleConns(10)
	sqldb.SetConnMaxLifetime(0)
	db := bun.NewDB(sqldb, sqlitedialect.New())
	repo := store.NewRepository(db)
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	dir := t.TempDir()
	sup := supervisor.New("/bin/sh", func(supervisor.Event) {})
	ports := portalloc.NewAllocator(20500, 20999)
	key := []byte("01234567890123456789012345678901"[:32])

	svc := New(repo, cf, sup, ports, key, dir)
	sup.OnEvent = svc.HandleSupervisorEvent // wire the callback after both exist
	return svc, repo
}

func TestCreateDomain_HappyPath(t *testing.T) {
	cf := &fakeCF{nextTunnelID: "tunnel-abc", nextDNSRecordID: "dns-abc"}
	svc, repo := newTestService(t, cf)

	d, err := svc.CreateDomain(context.Background(), "n8n.example.com", "http://n8n:5678")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	if d.CloudflareTunnelID != "tunnel-abc" {
		t.Errorf("CloudflareTunnelID = %q, want tunnel-abc", d.CloudflareTunnelID)
	}
	if d.EncryptedTunnelToken == "fake-token" {
		t.Error("tunnel token must be encrypted at rest, found plaintext")
	}

	stored, err := repo.Get(context.Background(), d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Hostname != "n8n.example.com" {
		t.Errorf("stored hostname = %q, want n8n.example.com", stored.Hostname)
	}
}

func TestCreateDomain_RollsBackOnDNSFailure(t *testing.T) {
	cf := &fakeCF{
		nextTunnelID:  "tunnel-xyz",
		createDNSErr:  errors.New("dns quota exceeded"),
	}
	svc, repo := newTestService(t, cf)

	_, err := svc.CreateDomain(context.Background(), "fail.example.com", "http://fail:80")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if len(cf.deletedTunnelIDs) != 1 || cf.deletedTunnelIDs[0] != "tunnel-xyz" {
		t.Errorf("deletedTunnelIDs = %v, want [tunnel-xyz]", cf.deletedTunnelIDs)
	}

	if _, err := repo.GetByHostname(context.Background(), "fail.example.com"); err == nil {
		t.Error("expected no row to be persisted after rollback, but one was found")
	}
}

func TestCreateDomain_DuplicateHostnameRejected(t *testing.T) {
	cf := &fakeCF{nextTunnelID: "t1", nextDNSRecordID: "d1"}
	svc, _ := newTestService(t, cf)

	if _, err := svc.CreateDomain(context.Background(), "dup.example.com", "http://a:80"); err != nil {
		t.Fatalf("first CreateDomain: %v", err)
	}
	if _, err := svc.CreateDomain(context.Background(), "dup.example.com", "http://b:80"); err == nil {
		t.Error("expected error creating duplicate hostname, got nil")
	}
}

func TestDeleteDomain_CleansUpCloudflareSide(t *testing.T) {
	cf := &fakeCF{nextTunnelID: "tunnel-del", nextDNSRecordID: "dns-del"}
	svc, repo := newTestService(t, cf)

	d, err := svc.CreateDomain(context.Background(), "del.example.com", "http://del:80")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}

	if err := svc.DeleteDomain(context.Background(), d.ID); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}

	if len(cf.deletedDNSIDs) != 1 || cf.deletedDNSIDs[0] != "dns-del" {
		t.Errorf("deletedDNSIDs = %v, want [dns-del]", cf.deletedDNSIDs)
	}
	if len(cf.deletedTunnelIDs) != 1 || cf.deletedTunnelIDs[0] != "tunnel-del" {
		t.Errorf("deletedTunnelIDs = %v, want [tunnel-del]", cf.deletedTunnelIDs)
	}
	if _, err := repo.Get(context.Background(), d.ID); err == nil {
		t.Error("expected domain row to be deleted, but Get succeeded")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/service/... -v`
Expected: FAIL — `service.New` undefined.

- [ ] **Step 3: Add the `OnEvent` field to `Supervisor` used by the test**

The test wires `sup.OnEvent = svc.HandleSupervisorEvent` after construction (since the service depends on the supervisor and the supervisor's callback needs the service, this breaks the construction-order cycle). Update Task 7's supervisor to expose `OnEvent` as a swappable exported field instead of only an unexported one set at `New`:

```go
// backend/internal/supervisor/supervisor.go — modify the Supervisor struct and New()
```

```go
type Supervisor struct {
	binary  string
	OnEvent func(Event)

	SleepFunc   func(time.Duration)
	StableAfter time.Duration

	mu    sync.Mutex
	procs map[string]*managedProcess
}

func New(binary string, onEvent func(Event)) *Supervisor {
	return &Supervisor{
		binary:      binary,
		OnEvent:     onEvent,
		SleepFunc:   time.Sleep,
		StableAfter: 60 * time.Second,
		procs:       make(map[string]*managedProcess),
	}
}
```

Every internal call site in `supervisor.go` that currently reads `s.onEvent(...)` must be updated to `s.OnEvent(...)`.

Run: `cd backend && go test ./internal/supervisor/... -v -timeout 30s`
Expected: PASS (confirms the rename didn't break Task 7's own tests)

- [ ] **Step 4: Implement the domain service**

```go
// backend/internal/service/domainservice.go
package service

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"tunnelmanager/internal/cfclient"
	"tunnelmanager/internal/crypto"
	"tunnelmanager/internal/logbuf"
	"tunnelmanager/internal/portalloc"
	"tunnelmanager/internal/store"
	"tunnelmanager/internal/supervisor"
)

type Service struct {
	repo   *store.Repository
	cf     cfclient.Client
	sup    *supervisor.Supervisor
	ports  *portalloc.Allocator
	encKey []byte
	logDir string
}

func New(repo *store.Repository, cf cfclient.Client, sup *supervisor.Supervisor, ports *portalloc.Allocator, encKey []byte, logDir string) *Service {
	return &Service{repo: repo, cf: cf, sup: sup, ports: ports, encKey: encKey, logDir: logDir}
}

func (s *Service) takenPorts(ctx context.Context) (map[int]bool, error) {
	domains, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	taken := make(map[int]bool, len(domains))
	for _, d := range domains {
		taken[d.MetricsPort] = true
	}
	return taken, nil
}

func (s *Service) CreateDomain(ctx context.Context, hostname, originURL string) (*store.Domain, error) {
	if existing, _ := s.repo.GetByHostname(ctx, hostname); existing != nil {
		return nil, fmt.Errorf("service: hostname %q already registered", hostname)
	}

	tunnel, err := s.cf.CreateTunnel(ctx, hostname)
	if err != nil {
		return nil, fmt.Errorf("service: create tunnel: %w", err)
	}

	if err := s.cf.PutIngressConfig(ctx, tunnel.TunnelID, hostname, originURL); err != nil {
		_ = s.cf.DeleteTunnel(ctx, tunnel.TunnelID)
		return nil, fmt.Errorf("service: put ingress config: %w", err)
	}

	dnsRecordID, err := s.cf.CreateDNSRecord(ctx, hostname, tunnel.TunnelID)
	if err != nil {
		_ = s.cf.DeleteTunnel(ctx, tunnel.TunnelID)
		return nil, fmt.Errorf("service: create dns record: %w", err)
	}

	encToken, err := crypto.Encrypt(s.encKey, tunnel.Token)
	if err != nil {
		_ = s.cf.DeleteDNSRecord(ctx, dnsRecordID)
		_ = s.cf.DeleteTunnel(ctx, tunnel.TunnelID)
		return nil, fmt.Errorf("service: encrypt token: %w", err)
	}

	taken, err := s.takenPorts(ctx)
	if err != nil {
		_ = s.cf.DeleteDNSRecord(ctx, dnsRecordID)
		_ = s.cf.DeleteTunnel(ctx, tunnel.TunnelID)
		return nil, fmt.Errorf("service: list taken ports: %w", err)
	}
	port, err := s.ports.Allocate(taken)
	if err != nil {
		_ = s.cf.DeleteDNSRecord(ctx, dnsRecordID)
		_ = s.cf.DeleteTunnel(ctx, tunnel.TunnelID)
		return nil, fmt.Errorf("service: allocate metrics port: %w", err)
	}

	now := time.Now().UTC()
	d := &store.Domain{
		ID:                   uuid.NewString(),
		Hostname:             hostname,
		OriginURL:            originURL,
		CloudflareTunnelID:   tunnel.TunnelID,
		DNSRecordID:          dnsRecordID,
		EncryptedTunnelToken: encToken,
		Status:               store.StatusPending,
		MetricsPort:          port,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := s.repo.Create(ctx, d); err != nil {
		_ = s.cf.DeleteDNSRecord(ctx, dnsRecordID)
		_ = s.cf.DeleteTunnel(ctx, tunnel.TunnelID)
		return nil, fmt.Errorf("service: persist domain: %w", err)
	}

	if err := s.spawn(ctx, d, tunnel.Token); err != nil {
		d.Status = store.StatusError
		d.LastError = err.Error()
		_ = s.repo.Update(ctx, d)
		// Cloudflare-side resources are already valid; a failed process
		// start is fixed via RestartDomain, not by recreating the tunnel.
		return d, nil
	}

	return d, nil
}

func (s *Service) spawn(ctx context.Context, d *store.Domain, plaintextToken string) error {
	logPath := filepath.Join(s.logDir, d.ID+".log")
	logWriter, err := logbuf.NewBuffer(logPath, 500)
	if err != nil {
		return fmt.Errorf("open log buffer: %w", err)
	}
	return s.sup.Start(d.ID, plaintextToken, d.MetricsPort, logWriter)
}

func (s *Service) ListDomains(ctx context.Context) ([]store.Domain, error) {
	return s.repo.List(ctx)
}

func (s *Service) GetDomain(ctx context.Context, id string) (*store.Domain, error) {
	return s.repo.Get(ctx, id)
}

func (s *Service) UpdateOrigin(ctx context.Context, id, originURL string) (*store.Domain, error) {
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.cf.PutIngressConfig(ctx, d.CloudflareTunnelID, d.Hostname, originURL); err != nil {
		return nil, fmt.Errorf("service: update ingress config: %w", err)
	}
	d.OriginURL = originURL
	if err := s.repo.Update(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) DeleteDomain(ctx context.Context, id string) error {
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if s.sup.IsRunning(id) {
		if err := s.sup.Stop(id); err != nil {
			return fmt.Errorf("service: stop process: %w", err)
		}
	}
	if err := s.cf.DeleteDNSRecord(ctx, d.DNSRecordID); err != nil {
		return fmt.Errorf("service: delete dns record: %w", err)
	}
	if err := s.cf.DeleteTunnel(ctx, d.CloudflareTunnelID); err != nil {
		return fmt.Errorf("service: delete tunnel: %w", err)
	}
	return s.repo.Delete(ctx, id)
}

func (s *Service) StopDomain(ctx context.Context, id string) error {
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.sup.Stop(id); err != nil {
		return err
	}
	d.Status = store.StatusStopped
	return s.repo.Update(ctx, d)
}

func (s *Service) RestartDomain(ctx context.Context, id string) error {
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	plaintext, err := crypto.Decrypt(s.encKey, d.EncryptedTunnelToken)
	if err != nil {
		return fmt.Errorf("service: decrypt token: %w", err)
	}
	d.RestartCount = 0
	d.LastError = ""
	if err := s.repo.Update(ctx, d); err != nil {
		return err
	}
	return s.spawn(ctx, d, plaintext)
}

// Reconcile is called once at boot: every domain the database says should be
// running gets its cloudflared process spawned again.
func (s *Service) Reconcile(ctx context.Context) error {
	active, err := s.repo.ListByStatus(ctx, store.StatusActive)
	if err != nil {
		return err
	}
	for i := range active {
		d := &active[i]
		plaintext, err := crypto.Decrypt(s.encKey, d.EncryptedTunnelToken)
		if err != nil {
			d.Status = store.StatusError
			d.LastError = fmt.Sprintf("reconcile: decrypt token: %v", err)
			_ = s.repo.Update(ctx, d)
			continue
		}
		if err := s.spawn(ctx, d, plaintext); err != nil {
			d.Status = store.StatusError
			d.LastError = err.Error()
			_ = s.repo.Update(ctx, d)
		}
	}
	return nil
}

// HandleSupervisorEvent is wired as the supervisor's OnEvent callback. It
// persists whatever the supervisor observed (process (re)started, crashed
// into error, or stopped intentionally).
func (s *Service) HandleSupervisorEvent(ev supervisor.Event) {
	ctx := context.Background()
	d, err := s.repo.Get(ctx, ev.DomainID)
	if err != nil {
		return
	}
	d.Status = ev.Status
	d.PID = ev.PID
	d.RestartCount = ev.RestartCount
	if ev.Err != nil {
		d.LastError = ev.Err.Error()
	}
	_ = s.repo.Update(ctx, d)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/service/... ./internal/supervisor/... -v -timeout 30s`
Expected: PASS (all `service` subtests plus the unchanged `supervisor` subtests)

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service backend/internal/supervisor
git commit -m "feat(backend): add domain service orchestration with Cloudflare rollback"
```

---

### Task 9: REST API — Gin handlers and router

**Files:**
- Create: `backend/internal/api/router.go`
- Create: `backend/internal/api/handlers.go`
- Test: `backend/internal/api/handlers_test.go`

**Interfaces:**
- Consumes: a `ServiceInterface` (defined in this task) satisfied by `*service.Service` (Task 8) in production and a hand-written fake in tests.
- Produces: `api.NewRouter(svc ServiceInterface) *gin.Engine`, consumed by Task 11 (`main.go`).

- [ ] **Step 1: Write the failing tests**

```go
// backend/internal/api/handlers_test.go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tunnelmanager/internal/store"
)

type fakeService struct {
	domains map[string]*store.Domain
	created *store.Domain
	err     error
}

func newFakeService() *fakeService {
	return &fakeService{domains: map[string]*store.Domain{}}
}

func (f *fakeService) CreateDomain(ctx context.Context, hostname, originURL string) (*store.Domain, error) {
	if f.err != nil {
		return nil, f.err
	}
	d := &store.Domain{
		ID: "new-id", Hostname: hostname, OriginURL: originURL,
		Status: store.StatusPending, EncryptedTunnelToken: "should-never-be-exposed",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	f.domains[d.ID] = d
	f.created = d
	return d, nil
}

func (f *fakeService) ListDomains(ctx context.Context) ([]store.Domain, error) {
	var out []store.Domain
	for _, d := range f.domains {
		out = append(out, *d)
	}
	return out, nil
}

func (f *fakeService) GetDomain(ctx context.Context, id string) (*store.Domain, error) {
	d, ok := f.domains[id]
	if !ok {
		return nil, errNotFound
	}
	return d, nil
}

func (f *fakeService) UpdateOrigin(ctx context.Context, id, originURL string) (*store.Domain, error) {
	d, ok := f.domains[id]
	if !ok {
		return nil, errNotFound
	}
	d.OriginURL = originURL
	return d, nil
}

func (f *fakeService) DeleteDomain(ctx context.Context, id string) error {
	if _, ok := f.domains[id]; !ok {
		return errNotFound
	}
	delete(f.domains, id)
	return nil
}

func (f *fakeService) StopDomain(ctx context.Context, id string) error {
	d, ok := f.domains[id]
	if !ok {
		return errNotFound
	}
	d.Status = store.StatusStopped
	return nil
}

func (f *fakeService) RestartDomain(ctx context.Context, id string) error {
	d, ok := f.domains[id]
	if !ok {
		return errNotFound
	}
	d.Status = store.StatusActive
	return nil
}

func TestCreateDomain_RedactsTunnelToken(t *testing.T) {
	svc := newFakeService()
	router := NewRouter(svc)

	body, _ := json.Marshal(map[string]string{"hostname": "n8n.example.com", "origin_url": "http://n8n:5678"})
	req := httptest.NewRequest(http.MethodPost, "/api/domains", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", w.Code, http.StatusCreated, w.Body.String())
	}
	if bytes.Contains(w.Body.Bytes(), []byte("should-never-be-exposed")) {
		t.Error("response body leaks the encrypted tunnel token")
	}

	var resp DomainResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Hostname != "n8n.example.com" {
		t.Errorf("Hostname = %q, want n8n.example.com", resp.Hostname)
	}
}

func TestListDomains_ReturnsAll(t *testing.T) {
	svc := newFakeService()
	svc.domains["a"] = &store.Domain{ID: "a", Hostname: "a.example.com", Status: store.StatusActive}
	router := NewRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/domains", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var resp []DomainResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp) != 1 || resp[0].Hostname != "a.example.com" {
		t.Errorf("resp = %+v, want one domain a.example.com", resp)
	}
}

func TestGetDomain_NotFound(t *testing.T) {
	svc := newFakeService()
	router := NewRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/domains/missing", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestDeleteDomain_Success(t *testing.T) {
	svc := newFakeService()
	svc.domains["a"] = &store.Domain{ID: "a", Hostname: "a.example.com"}
	router := NewRouter(svc)

	req := httptest.NewRequest(http.MethodDelete, "/api/domains/a", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -v`
Expected: FAIL — `NewRouter`, `DomainResponse`, `errNotFound` undefined.

- [ ] **Step 3: Implement the handlers and router**

```go
// backend/internal/api/handlers.go
package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"tunnelmanager/internal/store"
)

var errNotFound = errors.New("api: not found")

// ServiceInterface is the subset of *service.Service the HTTP layer needs.
// Defined here (not in the service package) so handler tests can supply a
// hand-written fake without importing service's own dependencies.
type ServiceInterface interface {
	CreateDomain(ctx context.Context, hostname, originURL string) (*store.Domain, error)
	ListDomains(ctx context.Context) ([]store.Domain, error)
	GetDomain(ctx context.Context, id string) (*store.Domain, error)
	UpdateOrigin(ctx context.Context, id, originURL string) (*store.Domain, error)
	DeleteDomain(ctx context.Context, id string) error
	StopDomain(ctx context.Context, id string) error
	RestartDomain(ctx context.Context, id string) error
}

// DomainResponse is the public shape returned by every endpoint. It
// deliberately has no field for the tunnel token.
type DomainResponse struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	OriginURL    string    `json:"origin_url"`
	Status       string    `json:"status"`
	MetricsPort  int       `json:"metrics_port"`
	PID          int       `json:"pid"`
	RestartCount int       `json:"restart_count"`
	LastError    string    `json:"last_error,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func toResponse(d *store.Domain) DomainResponse {
	return DomainResponse{
		ID: d.ID, Hostname: d.Hostname, OriginURL: d.OriginURL,
		Status: string(d.Status), MetricsPort: d.MetricsPort, PID: d.PID,
		RestartCount: d.RestartCount, LastError: d.LastError,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

type handlers struct {
	svc ServiceInterface
}

type createDomainRequest struct {
	Hostname  string `json:"hostname" binding:"required"`
	OriginURL string `json:"origin_url" binding:"required"`
}

func (h *handlers) createDomain(c *gin.Context) {
	var req createDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	d, err := h.svc.CreateDomain(c.Request.Context(), req.Hostname, req.OriginURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, toResponse(d))
}

func (h *handlers) listDomains(c *gin.Context) {
	domains, err := h.svc.ListDomains(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	resp := make([]DomainResponse, 0, len(domains))
	for i := range domains {
		resp = append(resp, toResponse(&domains[i]))
	}
	c.JSON(http.StatusOK, resp)
}

func (h *handlers) getDomain(c *gin.Context) {
	d, err := h.svc.GetDomain(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
	c.JSON(http.StatusOK, toResponse(d))
}

type updateDomainRequest struct {
	OriginURL string `json:"origin_url" binding:"required"`
}

func (h *handlers) updateDomain(c *gin.Context) {
	var req updateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	d, err := h.svc.UpdateOrigin(c.Request.Context(), c.Param("id"), req.OriginURL)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
	c.JSON(http.StatusOK, toResponse(d))
}

func (h *handlers) deleteDomain(c *gin.Context) {
	if err := h.svc.DeleteDomain(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) stopDomain(c *gin.Context) {
	if err := h.svc.StopDomain(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *handlers) restartDomain(c *gin.Context) {
	if err := h.svc.RestartDomain(c.Request.Context(), c.Param("id")); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
	c.Status(http.StatusNoContent)
}
```

```go
// backend/internal/api/router.go
package api

import "github.com/gin-gonic/gin"

func NewRouter(svc ServiceInterface) *gin.Engine {
	h := &handlers{svc: svc}
	r := gin.New()
	r.Use(gin.Recovery())

	g := r.Group("/api/domains")
	g.POST("", h.createDomain)
	g.GET("", h.listDomains)
	g.GET("/:id", h.getDomain)
	g.PUT("/:id", h.updateDomain)
	g.DELETE("/:id", h.deleteDomain)
	g.POST("/:id/stop", h.stopDomain)
	g.POST("/:id/restart", h.restartDomain)

	return r
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/api/... -v`
Expected: PASS (all four tests)

- [ ] **Step 5: Commit**

```bash
git add backend/internal/api
git commit -m "feat(backend): add REST API handlers and router"
```

---

### Task 10: Add logs and metrics endpoints

The spec lists `GET /api/domains/{id}/logs` and `GET /api/domains/{id}/metrics` as part of the REST API surface; Task 9 only wired the six CRUD/lifecycle endpoints. This task closes that gap before Task 11's `main.go` wiring, which assumes the full `ServiceInterface` including `Logs`/`ProxyMetrics`.

**Files:**
- Modify: `backend/internal/service/domainservice.go` (expose log lines and a metrics proxy)
- Modify: `backend/internal/api/handlers.go`, `backend/internal/api/router.go`
- Modify: `backend/internal/api/handlers_test.go`

**Interfaces:**
- `Service` gains `Logs(ctx, id string) ([]string, error)` and `ProxyMetrics(ctx context.Context, id string, w http.ResponseWriter) error`. `ServiceInterface` in the `api` package gains the same two methods.

- [ ] **Step 1: Write the failing tests**

```go
// append to backend/internal/api/handlers_test.go

func (f *fakeService) Logs(ctx context.Context, id string) ([]string, error) {
	if _, ok := f.domains[id]; !ok {
		return nil, errNotFound
	}
	return []string{"line one", "line two"}, nil
}

func (f *fakeService) ProxyMetrics(ctx context.Context, id string, w http.ResponseWriter) error {
	if _, ok := f.domains[id]; !ok {
		return errNotFound
	}
	w.Write([]byte("cloudflared_tunnel_requests_total 42\n"))
	return nil
}

func TestGetLogs_ReturnsLines(t *testing.T) {
	svc := newFakeService()
	svc.domains["a"] = &store.Domain{ID: "a", Hostname: "a.example.com"}
	router := NewRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/domains/a/logs", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var lines []string
	if err := json.Unmarshal(w.Body.Bytes(), &lines); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(lines) != 2 {
		t.Errorf("len(lines) = %d, want 2", len(lines))
	}
}

func TestGetMetrics_ProxiesRawText(t *testing.T) {
	svc := newFakeService()
	svc.domains["a"] = &store.Domain{ID: "a", Hostname: "a.example.com"}
	router := NewRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/domains/a/metrics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("cloudflared_tunnel_requests_total 42")) {
		t.Errorf("body = %q, want it to contain the metric line", w.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/api/... -v`
Expected: FAIL — `fakeService` does not implement the (not yet extended) `ServiceInterface`; compile error naming the missing methods.

- [ ] **Step 3: Extend `ServiceInterface` and add handlers**

```go
// backend/internal/api/handlers.go — add to ServiceInterface
```

```go
type ServiceInterface interface {
	CreateDomain(ctx context.Context, hostname, originURL string) (*store.Domain, error)
	ListDomains(ctx context.Context) ([]store.Domain, error)
	GetDomain(ctx context.Context, id string) (*store.Domain, error)
	UpdateOrigin(ctx context.Context, id, originURL string) (*store.Domain, error)
	DeleteDomain(ctx context.Context, id string) error
	StopDomain(ctx context.Context, id string) error
	RestartDomain(ctx context.Context, id string) error
	Logs(ctx context.Context, id string) ([]string, error)
	ProxyMetrics(ctx context.Context, id string, w http.ResponseWriter) error
}
```

```go
// backend/internal/api/handlers.go — add handler methods

func (h *handlers) getLogs(c *gin.Context) {
	lines, err := h.svc.Logs(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
	c.JSON(http.StatusOK, lines)
}

func (h *handlers) getMetrics(c *gin.Context) {
	if err := h.svc.ProxyMetrics(c.Request.Context(), c.Param("id"), c.Writer); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		return
	}
}
```

```go
// backend/internal/api/router.go — register the two new routes inside the existing group
	g.GET("/:id/logs", h.getLogs)
	g.GET("/:id/metrics", h.getMetrics)
```

- [ ] **Step 4: Implement `Logs` and `ProxyMetrics` on the real `Service`**

Logs need access to the `logbuf.Buffer` created per-domain in `spawn`; the service must keep a handle to it (it currently discards the buffer after passing it to the supervisor). Add a map to `Service`:

```go
// backend/internal/service/domainservice.go — add a field and update spawn/New

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"sync"
	"time"
	// ...existing imports
)

type Service struct {
	repo   *store.Repository
	cf     cfclient.Client
	sup    *supervisor.Supervisor
	ports  *portalloc.Allocator
	encKey []byte
	logDir string

	mu   sync.Mutex
	logs map[string]*logbuf.Buffer
}

func New(repo *store.Repository, cf cfclient.Client, sup *supervisor.Supervisor, ports *portalloc.Allocator, encKey []byte, logDir string) *Service {
	return &Service{repo: repo, cf: cf, sup: sup, ports: ports, encKey: encKey, logDir: logDir, logs: make(map[string]*logbuf.Buffer)}
}
```

```go
// backend/internal/service/domainservice.go — replace the body of spawn

func (s *Service) spawn(ctx context.Context, d *store.Domain, plaintextToken string) error {
	logPath := filepath.Join(s.logDir, d.ID+".log")
	logWriter, err := logbuf.NewBuffer(logPath, 500)
	if err != nil {
		return fmt.Errorf("open log buffer: %w", err)
	}
	s.mu.Lock()
	s.logs[d.ID] = logWriter
	s.mu.Unlock()
	return s.sup.Start(d.ID, plaintextToken, d.MetricsPort, logWriter)
}

func (s *Service) Logs(ctx context.Context, id string) ([]string, error) {
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

func (s *Service) ProxyMetrics(ctx context.Context, id string, w http.ResponseWriter) error {
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", d.MetricsPort))
	if err != nil {
		return fmt.Errorf("service: fetch metrics: %w", err)
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, err = io.Copy(w, resp.Body)
	return err
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/api/... ./internal/service/... -v -timeout 30s`
Expected: PASS across both packages.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/api backend/internal/service
git commit -m "feat(backend): add logs and metrics-proxy endpoints"
```

---

### Task 11: main.go wiring, boot reconciliation, graceful shutdown

**Files:**
- Create: `backend/cmd/server/main.go`

**Interfaces:**
- Consumes: `config.Load` (Task 1), `store.NewRepository`/`Migrate` (Task 3), `cfclient.New` (Task 6), `supervisor.New` (Task 7), `portalloc.NewAllocator` (Task 4), `service.New`/`Reconcile`/`HandleSupervisorEvent` (Task 8), `api.NewRouter` (Task 9).
- Produces: the `tunnel-manager` binary's entrypoint. Nothing downstream consumes this file — it is the top of the dependency graph.

- [ ] **Step 1: Write main.go**

There is no unit test for `main.go` itself — it is pure wiring. Correctness is verified by Task 12's manual Docker smoke test and Task 13's integration test, which exercise the same wiring path programmatically.

```go
// backend/cmd/server/main.go
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"

	"tunnelmanager/internal/api"
	"tunnelmanager/internal/cfclient"
	"tunnelmanager/internal/config"
	"tunnelmanager/internal/portalloc"
	"tunnelmanager/internal/service"
	"tunnelmanager/internal/store"
	"tunnelmanager/internal/supervisor"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := os.MkdirAll(cfg.LogDir, 0o755); err != nil {
		log.Fatalf("create log dir: %v", err)
	}

	sqldb, err := sql.Open("sqlite", "file:"+cfg.DBPath+"?cache=shared&mode=rwc")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	db := bun.NewDB(sqldb, sqlitedialect.New())
	repo := store.NewRepository(db)
	if err := repo.Migrate(context.Background()); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	cf := cfclient.New(cfg.CloudflareAPIToken, cfg.CloudflareAccountID, cfg.CloudflareZoneID)
	ports := portalloc.NewAllocator(cfg.MetricsPortRangeStart, cfg.MetricsPortRangeEnd)

	sup := supervisor.New(cfg.CloudflaredBinary, nil)
	svc := service.New(repo, cf, sup, ports, cfg.EncryptionKey, cfg.LogDir)
	sup.OnEvent = svc.HandleSupervisorEvent

	if err := svc.Reconcile(context.Background()); err != nil {
		log.Printf("reconcile: %v", err)
	}

	router := api.NewRouter(svc)
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: router}

	go func() {
		log.Printf("tunnel-manager listening on %s", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `cd backend && go build ./...`
Expected: no errors, produces `backend/tunnelmanager` (or whatever the default output name is) — safe to delete after confirming.

Run: `cd backend && rm -f tunnelmanager`

- [ ] **Step 3: Run the full test suite**

Run: `cd backend && go test ./... -v -timeout 60s`
Expected: PASS across every package.

- [ ] **Step 4: Commit**

```bash
git add backend/cmd
git commit -m "feat(backend): wire main entrypoint with boot reconciliation and graceful shutdown"
```

---

### Task 12: Dockerfile and docker-compose integration

**Files:**
- Create: `backend/Dockerfile`
- Create: `backend/.dockerignore`
- Modify: `docker-compose.yml` (add the `tunnel-manager` service)

**Interfaces:**
- Consumes: the `backend/` module built in Tasks 1–10.
- Produces: a running `tunnel-manager` container reachable on the docker-compose network, exposing its REST API on host port 8090.

- [ ] **Step 1: Write the Dockerfile**

```dockerfile
# backend/Dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/tunnel-manager ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl && \
    curl -fsSL -o /usr/local/bin/cloudflared \
      https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 && \
    chmod +x /usr/local/bin/cloudflared
COPY --from=builder /out/tunnel-manager /usr/local/bin/tunnel-manager
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/tunnel-manager"]
```

```
# backend/.dockerignore
tunnelmanager
*.db
*.log
```

- [ ] **Step 2: Build the image**

Run: `cd backend && docker build -t tunnel-manager:dev .`
Expected: build succeeds, final image runs `tunnel-manager` as entrypoint.

- [ ] **Step 3: Add the service to docker-compose.yml**

Read `docker-compose.yml` first to place this alongside the existing `9router`/`openclaw`/`cloudflared` services (same top-level `services:` key, same implicit default network so container-name DNS resolution works).

```yaml
  tunnel-manager:
    build: ./backend
    container_name: tunnel-manager
    environment:
      - CLOUDFLARE_API_TOKEN=${CLOUDFLARE_API_TOKEN}
      - CLOUDFLARE_ACCOUNT_ID=${CLOUDFLARE_ACCOUNT_ID}
      - CLOUDFLARE_ZONE_ID=${CLOUDFLARE_ZONE_ID}
      - ENCRYPTION_KEY=${TUNNEL_MANAGER_ENCRYPTION_KEY}
      - DB_PATH=/data/tunnel-manager.db
      - LOG_DIR=/data/logs
      - HTTP_ADDR=:8080
    volumes:
      - ./data/tunnel-manager:/data
    ports:
      - "8090:8080"
    restart: unless-stopped
```

- [ ] **Step 4: Verify the container starts and serves traffic**

```bash
mkdir -p data/tunnel-manager
# Generate a real 32-byte hex key instead of this placeholder before real use:
export TUNNEL_MANAGER_ENCRYPTION_KEY=$(openssl rand -hex 32)
docker compose up -d tunnel-manager
sleep 3
curl -s http://localhost:8090/api/domains
```

Expected: `[]` (empty JSON array — no domains created yet), and `docker compose logs tunnel-manager` shows `tunnel-manager listening on :8080` with no errors.

- [ ] **Step 5: Commit**

```bash
git add backend/Dockerfile backend/.dockerignore docker-compose.yml
git commit -m "feat(backend): add Dockerfile and wire tunnel-manager into docker-compose"
```

---

### Task 13: End-to-end integration test with a fake cloudflared binary

**Files:**
- Create: `backend/internal/service/integration_test.go`

**Interfaces:**
- Consumes: every package from Tasks 1–8. No new production interfaces are produced — this task only adds test coverage that exercises the full wiring path (service → supervisor → real `os/exec` → store) without needing a real Cloudflare account or the real `cloudflared` binary.

- [ ] **Step 1: Write the integration test**

```go
// backend/internal/service/integration_test.go
package service

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"

	"tunnelmanager/internal/portalloc"
	"tunnelmanager/internal/store"
	"tunnelmanager/internal/supervisor"
)

func TestIntegration_CreateThenCrashThenRestartThenDelete(t *testing.T) {
	// Fake cloudflared: stays up until sent SIGTERM.
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "fake-cloudflared.sh")
	script := "#!/bin/sh\ntrap 'exit 0' TERM\nwhile true; do sleep 0.1; done\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cloudflared: %v", err)
	}

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqldb.SetMaxIdleConns(10)
	sqldb.SetConnMaxLifetime(0)
	db := bun.NewDB(sqldb, sqlitedialect.New())
	repo := store.NewRepository(db)
	if err := repo.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cf := &fakeCF{nextTunnelID: "tunnel-e2e", nextDNSRecordID: "dns-e2e"}
	sup := supervisor.New(scriptPath, nil)
	ports := portalloc.NewAllocator(20500, 20999)
	key := []byte("01234567890123456789012345678901"[:32])
	logDir := t.TempDir()

	svc := New(repo, cf, sup, ports, key, logDir)
	sup.OnEvent = svc.HandleSupervisorEvent

	ctx := context.Background()
	d, err := svc.CreateDomain(ctx, "e2e.example.com", "http://e2e:80")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := repo.Get(ctx, d.ID)
		if got != nil && got.Status == store.StatusActive {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, err := repo.Get(ctx, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != store.StatusActive {
		t.Fatalf("status = %q, want active", got.Status)
	}

	if err := svc.DeleteDomain(ctx, d.ID); err != nil {
		t.Fatalf("DeleteDomain: %v", err)
	}
	if _, err := repo.Get(ctx, d.ID); err == nil {
		t.Error("expected domain to be gone after delete")
	}
	if len(cf.deletedDNSIDs) != 1 || len(cf.deletedTunnelIDs) != 1 {
		t.Errorf("expected one DNS delete and one tunnel delete, got dns=%v tunnels=%v", cf.deletedDNSIDs, cf.deletedTunnelIDs)
	}
}
```

- [ ] **Step 2: Run the integration test**

Run: `cd backend && go test ./internal/service/... -run TestIntegration -v -timeout 15s`
Expected: PASS

- [ ] **Step 3: Run the entire suite one final time**

Run: `cd backend && go test ./... -v -timeout 60s`
Expected: PASS across every package — this is the last checkpoint before considering the engine phase done.

- [ ] **Step 4: Commit**

```bash
git add backend/internal/service/integration_test.go
git commit -m "test(backend): add end-to-end create/crash/delete integration test"
```

---

## Plan Self-Review

**Spec coverage:**
- One tunnel per domain, remote-managed config → Task 6 (`cfclient`), Task 8 (`CreateDomain`).
- SQLite state → Task 3.
- Secrets (`CLOUDFLARE_API_TOKEN` never persisted, `ENCRYPTION_KEY` encrypts `tunnel_token`, token never in API responses) → Task 1 (config), Task 2 (crypto), Task 9 (`DomainResponse` has no token field, tested explicitly).
- Rollback on create failure → Task 8, `TestCreateDomain_RollsBackOnDNSFailure`.
- Update origin without restart → Task 8 `UpdateOrigin` (only calls `PutIngressConfig`, no supervisor interaction).
- Delete cascades to Cloudflare → Task 8 `DeleteDomain`, `TestDeleteDomain_CleansUpCloudflareSide`.
- Crash backoff (1s/2s/4s/8s/16s, 5 attempts, then error; 60s stability resets counter) → Task 7, `TestSupervisor_CrashLoopEndsInError`, `TestSupervisor_StableRunResetsRestartCount`.
- Intentional stop suppresses auto-restart → Task 7, `TestSupervisor_LongRunningProcessStaysActive`.
- Boot reconciliation → Task 8 `Reconcile`, Task 11 wiring (called before the HTTP server starts serving).
- REST API surface (all 8 endpoints from the spec, minus `/logs` and `/metrics`) → Task 9.
- Observability hooks: log ring buffer + file → Task 5, wired into every spawn in Task 8's `spawn` method. Metrics proxy endpoint (`GET /api/domains/{id}/metrics`) and `GET /api/domains/{id}/logs` are explicitly **not implemented in this plan** — see gap below.

**Gap found in self-review:** the spec lists `GET /api/domains/{id}/logs` and `GET /api/domains/{id}/metrics` as part of the REST API surface, but Task 9 as drafted only wires the six CRUD/lifecycle endpoints. Task 10 (inserted between Task 9 and Task 11 above, in its normal place in task order) closes this gap — Task 11's `main.go` wiring assumes the full `ServiceInterface` including `Logs`/`ProxyMetrics`.

**Placeholder scan:** no `TBD`/`TODO`/"add appropriate" phrasing anywhere above; every step shows complete code or an exact command.

**Type consistency check:** `store.Domain`, `store.Status`, `cfclient.Client`/`TunnelInfo`, `supervisor.Event`/`Supervisor` (including the `OnEvent`/`SleepFunc`/`StableAfter` exported fields introduced in Task 8's Step 3), `service.Service` methods, and `api.ServiceInterface`/`DomainResponse` are named and typed identically everywhere they are used across tasks — cross-checked against each "Interfaces" block while writing this plan.

**Scope check:** this plan covers exactly the engine sub-project from the spec. The Next.js UI and the Prometheus/Grafana/Loki stack are separate specs and separate plans, per the spec's own Non-goals section.

**End-to-end verification performed while writing this plan:** every code block above (Tasks 1–9b, final state after all modifications) was assembled into a real Go module and run with `go build ./...` and `go test ./... -timeout 60s`. This surfaced three bugs that are already fixed in the code shown above — noted here so a reader doesn't wonder why the code looks slightly more defensive than a first draft would:

1. Task 1's test fixture hex string was 62 characters (31 bytes) instead of 64 (32 bytes) — a copy-paste truncation. Fixed to the full 64-character string shown in Step 2.
2. Task 3's (and Task 8's, and Task 13's) test helper originally opened SQLite with the anonymous DSN `file::memory:?cache=shared`. Go runs all tests in a package in one process, and that DSN's shared cache is keyed by name — every test using the literal same anonymous DSN string shares one underlying in-memory database, so rows from one test leaked into the next (`TestRepository_UpdateAndListByStatus` saw a row left over from `TestRepository_CreateAndGet`). Fixed by deriving a per-test DSN from `t.Name()`, shown in every test helper above.
3. Task 7's original `TestSupervisor_StableRunResetsRestartCount` waited for `status=error` after configuring a `StableAfter` shorter than the crash cycle — which means the reset logic (working correctly) prevents `status=error` from ever being reached, so the original test could only time out. Rewritten to assert on `RestartCount` staying at 1 across two consecutive crash/respawn cycles instead of waiting for a terminal state that a working implementation never reaches.

A worker executing this plan task-by-task will therefore hit passing tests on the first implementation attempt, rather than rediscovering these three issues independently.
