# Domain Detail SSE and Multi-Zone Frontend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace frontend domain/log/metric polling with native SSE and make domain creation select and submit a Cloudflare `zoneId`.

**Architecture:** Backend keeps existing REST endpoints and domain-list stream, adds one authenticated per-domain stream carrying bounded log snapshots and four-second metric snapshots, and uses process-local non-blocking log notifications. Next.js same-origin proxy streams backend bodies and forwards SSE headers; client providers validate events and synchronize TanStack Query caches while REST reads remain one-shot bootstrap/fallback requests.

**Tech Stack:** Go 1.26 standard library, Gin 1.12, Next.js 16.2.10 App Router, React 19.2.4, TanStack Query 5.101.2, TypeScript 5, Node test runner.

## Global Constraints

- No new third-party dependency.
- Keep `GET /api/domains/:id/logs` and `GET /api/domains/:id/metrics` compatible.
- Keep `GET /api/domains/stream` full-snapshot behavior compatible.
- Add `GET /api/domains/:id/stream`; event names are exactly `logs`, `metrics`, and `metrics-error`.
- Log events contain full snapshots capped at 500 complete lines; partial lines do not publish.
- Metric snapshots send immediately and every four seconds; heartbeat interval is 15 seconds.
- Metric failures emit `{"message":"metrics unavailable"}` and do not close detail stream.
- Native `EventSource` only; JWT remains hidden in `HttpOnly` session cookie and Next.js BFF.
- Preserve FE `origin/main` path/read-only-domain work from commit `b662cda`; add `zoneId` without reverting `path`, `managed`, or `cloudflareStatus` fields.
- FE local `main` is behind `origin/main`; create FE implementation worktree/branch from `origin/main`, not local `main` at `2cf945a`.
- Domain snapshots from current backend may omit newer FE-only `path`, `managed`, and `cloudflareStatus`; normalize them to `""`, `true`, and `""` respectively.
- Backend broadcaster and log subscriptions remain process-local. Add shared pub/sub only before multiple backend replicas need cross-replica updates.
- Reference design: `docs/superpowers/specs/2026-07-29-domain-detail-sse-and-multi-zone-frontend-design.md`.
- Before FE edits, read relevant Next.js 16 guides in `node_modules/next/dist/docs/01-app/03-api-reference/03-file-conventions/route.md` and `node_modules/next/dist/docs/01-app/02-guides/backend-for-frontend.md`.

## File Map

### Backend repository: `/home/kuon/code/tunnel-manager`

- `internal/application/api/route/domain/route.go`: fix zones auth wiring and register detail stream.
- `internal/application/api/route/domain/detail_stream.go`: own detail SSE headers, event encoding, tickers, and cancellation.
- `internal/application/api/route/domain/detail_stream_test.go`: exercise stream through a real HTTP server/client.
- `internal/application/api/route/domain/stream_test.go`: extend shared fake domain service only where new interface methods require it.
- `internal/application/api/route/domain/route_test.go`: verify zones and detail stream remain authenticated.
- `internal/services/domain/service.go`: expose log subscription and metric snapshot contracts; hold bounded metrics HTTP client.
- `internal/services/domain/service_test.go`: test context-aware metric fetches and stable log subscriptions.
- `internal/services/domain/create.go`: reuse stable log buffers instead of replacing subscribed buffers.
- `internal/pkg/logbuf/logbuf.go`: add atomic snapshot/subscription and non-blocking complete-line notifications.
- `internal/pkg/logbuf/logbuf_test.go`: test partial lines, cap, coalescing, and cancellation.
- `README.md`: document detail SSE endpoint and payloads.

### Frontend repository: `/home/kuon/code/tunnel-manager-fe`

- `lib/server/backend-core.ts`: propagate abort signal and whitelist SSE response headers.
- `lib/server/backend-core.test.ts`: test signal forwarding, response headers, and session-response body cancellation.
- `lib/server/backend.ts`: construct proxied `Response` with whitelisted headers.
- `app/api/session/route.ts`: add authenticated session validation `GET` used after SSE transport errors.
- `lib/domain-stream.ts`: define stream keys, payload validators/normalizers, cache application, session checker, and token-change signal.
- `lib/domain-stream.test.ts`: test trust-boundary parsing, cache convergence, cleanup helpers, and stale request cancellation.
- `components/domain-stream-provider.tsx`: own one authenticated-layout domain-list `EventSource`.
- `hooks/use-domain-detail-stream.ts`: own one detail `EventSource` per mounted domain page.
- `hooks/use-domains.ts`: consume shared keys, remove recurring polling, add zones query.
- `app/(authenticated)/layout.tsx`: mount global stream provider once.
- `components/domain-detail.tsx`: mount detail stream hook for current ID.
- `lib/api.ts`: add `zoneId`, zone API, and password-change stream restart signal while preserving upstream path/read-only fields.
- `lib/api-config.ts`: extend create payload with `zoneId` while preserving `path`.
- `lib/api-config.test.ts`: verify exact create payload.
- `components/create-domain-dialog.tsx`: load zones on open, default selection, native accessible selector, and disabled states.

---

### Task 1: Restore Authenticated Backend Route Baseline

**Files:**
- Modify: `internal/application/api/route/domain/route.go:37-52`
- Modify: `internal/application/api/route/domain/route_test.go`

**Interfaces:**
- Consumes: `middleware.JWTAuth(authenticator middleware.Authenticator, cfg config.Config) gin.HandlerFunc`.
- Produces: compiling `/api/cloudflare/zones` registration using `r.cfg`; no new public interface.

- [ ] **Step 1: Add route authentication regression test**

Add a second table entry so both zones and future detail stream paths reject missing credentials. For this task, lock existing zones behavior first:

```go
func TestZoneListRequiresJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	handler := &DomainHandler{domainService: &fakeRouteDomainService{}}
	route := &DomainRoute{
		Engine:        engine,
		domainHandler: handler,
		authService:   &fakeRouteAuthService{},
		cfg:           config.Config{},
	}
	route.Setup()

	request := httptest.NewRequest(http.MethodGet, "/api/cloudflare/zones", nil)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}
```

Add `tunnelmanager/internal/pkg/config` to imports.

- [ ] **Step 2: Run test to expose compile failure**

Run from backend repo:

```bash
go test ./internal/application/api/route/domain -run TestZoneListRequiresJWT -count=1
```

Expected: compile failure at `middleware.JWTAuth(r.authService)` because current function requires `config.Config`.

- [ ] **Step 3: Apply one-line route fix**

Change zones group registration:

```go
cloudflare := r.Group("/api/cloudflare", middleware.JWTAuth(r.authService, r.cfg))
```

- [ ] **Step 4: Verify route package**

```bash
go test ./internal/application/api/route/domain -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit backend baseline fix**

```bash
git add internal/application/api/route/domain/route.go internal/application/api/route/domain/route_test.go
git commit -m "fix: authenticate Cloudflare zones route"
```

### Task 2: Add Non-Blocking Log Buffer Subscriptions

**Files:**
- Modify: `internal/pkg/logbuf/logbuf.go`
- Create: `internal/pkg/logbuf/logbuf_test.go`

**Interfaces:**
- Consumes: existing `NewBuffer(filePath string, capacity int) (*Buffer, error)`, `Write`, `Lines`, and `Close`.
- Produces: `func (b *Buffer) SnapshotAndSubscribe() ([]string, <-chan struct{}, func())`.
- Produces: each successful write that completes one or more lines publishes one coalesced signal; partial-only writes publish none.

- [ ] **Step 1: Write failing log subscription tests**

Create `internal/pkg/logbuf/logbuf_test.go`:

```go
package logbuf

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotAndSubscribePublishesCompleteLinesAndCapsSnapshot(t *testing.T) {
	buf, err := NewBuffer(filepath.Join(t.TempDir(), "domain.log"), 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = buf.Close() })

	initial, updates, cancel := buf.SnapshotAndSubscribe()
	defer cancel()
	if len(initial) != 0 {
		t.Fatalf("initial = %#v", initial)
	}

	if _, err := buf.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
		t.Fatal("partial line published")
	default:
	}

	if _, err := buf.Write([]byte(" one\nsecond\nthird\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("complete lines did not publish")
	}
	if got := buf.Lines(); len(got) != 2 || got[0] != "second" || got[1] != "third" {
		t.Fatalf("lines = %#v", got)
	}
}

func TestLogNotificationsCoalesceAndCancellationIsIdempotent(t *testing.T) {
	buf, err := NewBuffer(filepath.Join(t.TempDir(), "domain.log"), 500)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = buf.Close() })

	_, updates, cancel := buf.SnapshotAndSubscribe()
	if _, err := buf.Write([]byte("one\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := buf.Write([]byte("two\n")); err != nil {
		t.Fatal(err)
	}
	<-updates
	select {
	case <-updates:
		t.Fatal("notifications did not coalesce")
	default:
	}

	cancel()
	cancel()
	if _, err := buf.Write([]byte("three\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
		t.Fatal("cancelled subscriber received notification")
	default:
	}
}
```

- [ ] **Step 2: Run tests to verify missing API**

```bash
go test ./internal/pkg/logbuf -count=1
```

Expected: FAIL with `buf.SnapshotAndSubscribe undefined`.

- [ ] **Step 3: Implement minimum subscription state**

Extend `Buffer` and constructor:

```go
type Buffer struct {
	mu          sync.Mutex
	capacity    int
	lines       []string
	partial     string
	file        *os.File
	writer      *bufio.Writer
	subscribers map[chan struct{}]struct{}
}
```

```go
return &Buffer{
	capacity:    capacity,
	file:        f,
	writer:      bufio.NewWriter(f),
	subscribers: make(map[chan struct{}]struct{}),
}, nil
```

Track whether `Write` completed any line and publish while holding `b.mu`; sends remain non-blocking:

```go
completed := false
b.partial += string(p)
for {
	idx := strings.IndexByte(b.partial, '\n')
	if idx < 0 {
		break
	}
	completed = true
	line := b.partial[:idx]
	b.partial = b.partial[idx+1:]
	b.lines = append(b.lines, line)
	if len(b.lines) > b.capacity {
		b.lines = b.lines[len(b.lines)-b.capacity:]
	}
}
if completed {
	for subscriber := range b.subscribers {
		select {
		case subscriber <- struct{}{}:
		default:
		}
	}
}
```

Add atomic snapshot/subscription:

```go
func (b *Buffer) SnapshotAndSubscribe() ([]string, <-chan struct{}, func()) {
	b.mu.Lock()
	updates := make(chan struct{}, 1)
	b.subscribers[updates] = struct{}{}
	lines := make([]string, len(b.lines))
	copy(lines, b.lines)
	b.mu.Unlock()

	var once sync.Once
	return lines, updates, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, updates)
			b.mu.Unlock()
		})
	}
}
```

Do not close subscriber channels; deletion plus idempotent cancellation avoids send/close races.

- [ ] **Step 4: Run log buffer tests**

```bash
go test ./internal/pkg/logbuf -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit log subscription primitive**

```bash
git add internal/pkg/logbuf/logbuf.go internal/pkg/logbuf/logbuf_test.go
git commit -m "feat: publish completed log lines"
```

### Task 3: Expose Stable Logs and Context-Aware Metrics

**Files:**
- Modify: `internal/services/domain/service.go`
- Modify: `internal/services/domain/create.go:103-120`
- Modify: `internal/services/domain/service_test.go`

**Interfaces:**
- Consumes: `logbuf.Buffer.SnapshotAndSubscribe` from Task 2 and existing domain repository.
- Produces: `SubscribeLogs(ctx context.Context, id string) ([]string, <-chan struct{}, func(), error)`.
- Produces: `Metrics(ctx context.Context, id string) (string, error)`.
- Preserves: `Logs(ctx, id)` and `ProxyMetrics(ctx, id, w)` REST contracts.

- [ ] **Step 1: Write failing stable-log and metric tests**

Extend service tests with stdlib-only cases. Use a local test server port because production metric URL remains `http://localhost:<port>/metrics`:

```go
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

func TestMetricsReturnsPrometheusTextAndPropagatesCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	_, portText, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	service := newTestDomainService(t, newFakeDomainRepo(&model.Domain{ID: "domain-1", MetricsPort: port}), &fakeCloudflareClient{})
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
```

Also add a separate success server returning `# HELP tunnel_up\n` and assert `Metrics` returns exact text. Add imports: `net`, `net/http`, `net/http/httptest`, `strconv`, `strings`, and `time`.

- [ ] **Step 2: Run focused service tests**

```bash
go test ./internal/services/domain -run 'Test(SubscribeLogs|Metrics)' -count=1
```

Expected: FAIL because `SubscribeLogs` and `Metrics` are undefined.

- [ ] **Step 3: Add stable log buffer helper and service interface**

Add interface methods:

```go
SubscribeLogs(ctx context.Context, id string) ([]string, <-chan struct{}, func(), error)
Metrics(ctx context.Context, id string) (string, error)
```

Add a three-second stdlib client to `domainService` and initialize it:

```go
metricsClient *http.Client
```

```go
metricsClient: &http.Client{Timeout: 3 * time.Second},
```

Add stable buffer lookup:

```go
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
```

Update `Logs` to use existing map without creating a buffer, preserving empty-array REST behavior.

- [ ] **Step 4: Reuse buffer from process spawn**

Replace buffer creation/replacement in `spawn`:

```go
func (s *domainService) spawn(domain *model.Domain, plaintextToken string) error {
	logWriter, err := s.logBuffer(domain.ID)
	if err != nil {
		return fmt.Errorf("open log buffer: %w", err)
	}
	if err := s.sup.Start(domain.ID, plaintextToken, domain.MetricsPort, logWriter); err != nil {
		return err
	}
	return nil
}
```

This keeps subscriptions attached when stream opens before process spawn or restart.

- [ ] **Step 5: Refactor metric retrieval while preserving REST response**

Implement snapshot retrieval:

```go
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
```

- [ ] **Step 6: Run service and repository-facing tests**

```bash
go test ./internal/services/domain ./internal/pkg/logbuf -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit service stream primitives**

```bash
git add internal/services/domain/service.go internal/services/domain/create.go internal/services/domain/service_test.go
git commit -m "feat: expose domain log and metric snapshots"
```

### Task 4: Add Authenticated Domain Detail SSE Endpoint

**Files:**
- Create: `internal/application/api/route/domain/detail_stream.go`
- Create: `internal/application/api/route/domain/detail_stream_test.go`
- Modify: `internal/application/api/route/domain/route.go`
- Modify: `internal/application/api/route/domain/route_test.go`
- Modify: `README.md:178-275`

**Interfaces:**
- Consumes: `SubscribeLogs`, `Logs`, and `Metrics` from Task 3.
- Produces: authenticated `GET /api/domains/:id/stream`.
- Produces payloads: `logs` as `{"items":[]}`, `metrics` as `{"text":"..."}`, `metrics-error` as `{"message":"metrics unavailable"}`.

- [ ] **Step 1: Write failing real-HTTP stream tests**

Create `detail_stream_test.go` using existing `readSSEEvent` helper and `httptest.NewServer`. Extend `fakeDomainService` with fields and methods:

```go
logLines       []string
logUpdates     chan struct{}
logCancelled   chan struct{}
metricText     string
metricErr      error
metricCalls    int
```

```go
func (f *fakeDomainService) SubscribeLogs(context.Context, string) ([]string, <-chan struct{}, func(), error) {
	return f.logLines, f.logUpdates, func() {
		f.cancelOnce.Do(func() { close(f.logCancelled) })
	}, nil
}

func (f *fakeDomainService) Logs(context.Context, string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.logLines...), nil
}

func (f *fakeDomainService) Metrics(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.metricCalls++
	return f.metricText, f.metricErr
}
```

Core initial snapshot test:

```go
func TestStreamDomainDetailSendsInitialLogsAndMetrics(t *testing.T) {
	service := &fakeDomainService{
		logLines:     []string{"started"},
		logUpdates:   make(chan struct{}, 1),
		logCancelled: make(chan struct{}),
		metricText:   "# HELP tunnel_up\n",
	}
	resp, reader, cancel := openDomainDetailStream(t, service, "domain-1")
	defer cancel()
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content type = %q", got)
	}
	if resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("headers = %#v", resp.Header)
	}
	logs := readSSEEvent(t, reader)
	metrics := readSSEEvent(t, reader)
	if logs.name != "logs" || logs.data != `{"items":["started"]}` {
		t.Fatalf("logs = %#v", logs)
	}
	if metrics.name != "metrics" || metrics.data != `{"text":"# HELP tunnel_up\n"}` {
		t.Fatalf("metrics = %#v", metrics)
	}
}
```

Add tests for:

- Add `logErr error` to `fakeDomainService`; make `SubscribeLogs` return it. Assert `model.ErrNotFound` produces HTTP 404 JSON before SSE headers.
- A log notification reloads and sends a new `logs` snapshot.
- Initial metric failure sends exact `metrics-error` and stream remains open for a later log event.
- A shortened `domainDetailMetricsInterval` causes a second `metrics` call/event.
- A shortened `domainDetailHeartbeatInterval` emits `: heartbeat`.
- Request cancellation closes `logCancelled` within one second.

- [ ] **Step 2: Run tests to verify missing handler**

```bash
go test ./internal/application/api/route/domain -run StreamDomainDetail -count=1
```

Expected: FAIL because `streamDomainDetail` and interval variables are undefined.

- [ ] **Step 3: Implement detail stream handler**

Create `detail_stream.go`:

```go
package domainroute

import (
	"errors"
	"io"
	"net/http"
	"time"

	"tunnelmanager/internal/model"

	"github.com/gin-gonic/gin"
)

var (
	domainDetailMetricsInterval   = 4 * time.Second
	domainDetailHeartbeatInterval = 15 * time.Second
)

type logSnapshot struct {
	Items []string `json:"items"`
}

type metricSnapshot struct {
	Text string `json:"text"`
}

func (h *DomainHandler) streamDomainDetail(c *gin.Context) {
	ctx := c.Request.Context()
	id := c.Param("id")
	lines, updates, cancel, err := h.domainService.SubscribeLogs(ctx, id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	defer cancel()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")

	initial := true
	metricsTicker := time.NewTicker(domainDetailMetricsInterval)
	heartbeat := time.NewTicker(domainDetailHeartbeatInterval)
	defer metricsTicker.Stop()
	defer heartbeat.Stop()

	writeMetrics := func() {
		metrics, err := h.domainService.Metrics(ctx, id)
		if err != nil {
			c.SSEvent("metrics-error", gin.H{"message": "metrics unavailable"})
			return
		}
		c.SSEvent("metrics", metricSnapshot{Text: metrics})
	}

	c.Stream(func(w io.Writer) bool {
		if initial {
			initial = false
			c.SSEvent("logs", logSnapshot{Items: lines})
			writeMetrics()
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-updates:
			lines, err = h.domainService.Logs(ctx, id)
			if err != nil {
				c.SSEvent("error", gin.H{"message": "stream unavailable"})
				return false
			}
			c.SSEvent("logs", logSnapshot{Items: lines})
		case <-metricsTicker.C:
			writeMetrics()
		case <-heartbeat.C:
			_, _ = io.WriteString(w, ": heartbeat\n\n")
		}
		return true
	})
}
```

- [ ] **Step 4: Register route and verify auth**

Register under existing authenticated group:

```go
g.GET("/:id/stream", r.domainHandler.streamDomainDetail)
```

Extend route auth test with `/api/domains/domain-1/stream`; expected status is 401 without token. Keep static `/stream` registration before parameter routes.

- [ ] **Step 5: Document endpoint contract**

Add API table row:

```markdown
| `GET` | `/api/domains/:id/stream` | Stream bounded logs and raw Prometheus metrics using SSE. |
```

Add a concise detail stream section containing exact event examples and timing:

```text
event: logs
data: {"items":["line 1","line 2"]}

event: metrics
data: {"text":"# HELP tunnel_up\n"}

event: metrics-error
data: {"message":"metrics unavailable"}
```

State logs are current 500-line snapshots, metrics retry every four seconds, heartbeat is 15 seconds, and reconnect gets fresh snapshots.

- [ ] **Step 6: Verify endpoint and full backend**

```bash
gofmt -w internal/application/api/route/domain internal/services/domain internal/pkg/logbuf
go test ./...
go build ./...
```

Expected: all commands PASS.

- [ ] **Step 7: Commit backend detail stream**

```bash
git add internal/application/api/route/domain/detail_stream.go internal/application/api/route/domain/detail_stream_test.go internal/application/api/route/domain/route.go internal/application/api/route/domain/route_test.go internal/application/api/route/domain/stream_test.go README.md
git commit -m "feat: stream domain logs and metrics"
```

### Task 5: Make Next.js Proxy Preserve SSE Lifecycle

**Files:**
- Modify: `lib/server/backend-core.ts`
- Modify: `lib/server/backend-core.test.ts`
- Modify: `lib/server/backend.ts`
- Modify: `app/api/session/route.ts`

**Interfaces:**
- Consumes: Web `Request`, `Response`, `AbortSignal`, and existing session token helpers.
- Produces: `proxyResponseHeaders(upstream: Headers): Headers`.
- Produces: `toSessionValidationResponse(response: Response): Promise<Response>`.
- Produces: authenticated `GET /api/session` returning 204 for a valid backend session and upstream status otherwise.

- [ ] **Step 1: Start FE branch from current remote main**

From FE repository, fetch and create isolated implementation branch/worktree from `origin/main` (`b662cda`). Do not implement against stale local `main` (`2cf945a`). Confirm these files exist after checkout: `lib/domain-view.ts` and `lib/domain-view.test.ts`.

- [ ] **Step 2: Write failing proxy tests**

Extend `backend-core.test.ts` imports:

```ts
import {
  backendURL,
  isSameOrigin,
  proxyRequestInit,
  proxyResponseHeaders,
  toSessionValidationResponse,
} from "./backend-core.ts";
```

Add assertions:

```ts
test("proxy request forwards browser cancellation", async () => {
  const controller = new AbortController();
  const request = new Request("http://localhost:3000/api/domains/stream", {
    signal: controller.signal,
  });
  const init = await proxyRequestInit(request, "trusted");
  assert.equal(init.signal, request.signal);
});

test("proxy response preserves only streaming headers", () => {
  const headers = proxyResponseHeaders(new Headers({
    "content-type": "text/event-stream",
    "cache-control": "no-cache",
    "x-accel-buffering": "no",
    "set-cookie": "secret=true",
  }));
  assert.deepEqual(Object.fromEntries(headers), {
    "cache-control": "no-cache",
    "content-type": "text/event-stream",
    "x-accel-buffering": "no",
  });
});

test("session validation cancels body and keeps upstream status", async () => {
  let cancelled = false;
  const body = new ReadableStream({ cancel() { cancelled = true; } });
  const valid = await toSessionValidationResponse(new Response(body, { status: 200 }));
  assert.equal(valid.status, 204);
  assert.equal(cancelled, true);

  const unauthorized = await toSessionValidationResponse(new Response(null, { status: 401 }));
  assert.equal(unauthorized.status, 401);
});
```

- [ ] **Step 3: Run focused tests**

```bash
pnpm test
```

Expected: FAIL because response helpers are undefined and request signal is not forwarded.

- [ ] **Step 4: Implement proxy helpers**

Include `signal` in returned `RequestInit`:

```ts
return {
  method: request.method,
  headers,
  body: body || undefined,
  redirect: "manual",
  signal: request.signal,
};
```

Add whitelisted response headers and session conversion:

```ts
const PROXY_RESPONSE_HEADERS = ["content-type", "cache-control", "x-accel-buffering"] as const;

export function proxyResponseHeaders(upstream: Headers): Headers {
  const headers = new Headers();
  for (const name of PROXY_RESPONSE_HEADERS) {
    const value = upstream.get(name);
    if (value) headers.set(name, value);
  }
  return headers;
}

export async function toSessionValidationResponse(response: Response): Promise<Response> {
  await response.body?.cancel();
  return new Response(null, { status: response.ok ? 204 : response.status });
}
```

Use `proxyResponseHeaders(response.headers)` in `proxyBackend` instead of copying only content type.

- [ ] **Step 5: Add session validation route**

Add `GET` to `app/api/session/route.ts`:

```ts
export async function GET(request: NextRequest) {
  const validationRequest = new Request(new URL("?pageSize=1", request.url), {
    signal: request.signal,
  });
  return toSessionValidationResponse(await proxyBackend(validationRequest, ["domains"]));
}
```

Import `toSessionValidationResponse`. Existing `DELETE` stays unchanged.

- [ ] **Step 6: Verify proxy layer**

```bash
pnpm test
pnpm lint
```

Expected: PASS.

- [ ] **Step 7: Commit FE proxy support**

```bash
git add lib/server/backend-core.ts lib/server/backend-core.test.ts lib/server/backend.ts app/api/session/route.ts
git commit -m "feat: proxy authenticated SSE responses"
```

### Task 6: Build Validated Stream-to-Cache Core

**Files:**
- Create: `lib/domain-stream.ts`
- Create: `lib/domain-stream.test.ts`
- Modify: `lib/api.ts`

**Interfaces:**
- Consumes: `Domain` from `lib/api.ts` and TanStack `QueryClient`.
- Produces: `domainKeys`, `parseDomainSnapshot`, `parseLogSnapshot`, `parseMetricSnapshot`, `applyDomainSnapshot`, `applyLogSnapshot`, `applyMetricSnapshot`.
- Produces: `bindDomainListSource`, `bindDomainDetailSource`, `createSessionChecker`, `notifySessionTokenChanged`, and `subscribeToSessionTokenChanges`.

- [ ] **Step 1: Write failing trust-boundary and cache tests**

Create `lib/domain-stream.test.ts` with a complete domain matching FE `origin/main`, now including `zoneId`:

```ts
const domain: Domain = {
  id: "domain-1",
  hostname: "app.example.com",
  originUrl: "http://localhost:3001",
  zoneId: "zone-1",
  path: "",
  managed: true,
  cloudflareStatus: "",
  status: "active",
  metricsPort: 20500,
  pid: 42,
  restartCount: 1,
  lastError: "",
  createdAt: "2026-07-29T10:00:00Z",
  updatedAt: "2026-07-29T10:01:00Z",
  cloudflareTunnelId: "tunnel-1",
  dnsRecordId: "dns-1",
};
```

Test current backend normalization:

```ts
test("domain parser validates backend fields and normalizes newer optional fields", () => {
  const { path, managed, cloudflareStatus, ...backendDomain } = domain;
  assert.deepEqual(
    parseDomainSnapshot(JSON.stringify({ items: [backendDomain], nextCursor: "" })),
    { items: [{ ...backendDomain, path: "", managed: true, cloudflareStatus: "" }], nextCursor: "" },
  );
  assert.equal(parseDomainSnapshot("not-json"), undefined);
  assert.equal(parseDomainSnapshot(JSON.stringify({ items: [{ id: "broken" }], nextCursor: "" })), undefined);
});

test("detail parsers reject malformed payloads", () => {
  assert.deepEqual(parseLogSnapshot('{"items":["one","two"]}'), ["one", "two"]);
  assert.equal(parseLogSnapshot('{"items":[1]}'), undefined);
  assert.equal(parseMetricSnapshot('{"text":"# HELP up\\n"}'), "# HELP up\n");
  assert.equal(parseMetricSnapshot('{"text":1}'), undefined);
});
```

Test cache convergence:

```ts
test("complete domain snapshot replaces list, updates details, and removes deleted details", async () => {
  const queryClient = new QueryClient();
  const deleted = { ...domain, id: "domain-2" };
  queryClient.setQueryData(domainKeys.all, [domain, deleted]);
  queryClient.setQueryData(domainKeys.detail(domain.id), { ...domain, hostname: "old.example.com" });
  queryClient.setQueryData(domainKeys.detail(deleted.id), deleted);

  await applyDomainSnapshot(queryClient, { items: [domain], nextCursor: "" });

  assert.deepEqual(queryClient.getQueryData(domainKeys.all), [domain]);
  assert.deepEqual(queryClient.getQueryData(domainKeys.detail(domain.id)), domain);
  assert.equal(queryClient.getQueryData(domainKeys.detail(deleted.id)), undefined);
});
```

Add tests that log/metric cache setters cancel stale in-flight queries before setting values, malformed event data leaves old cache untouched, bound source cleanup removes listeners and calls `close`, session checker ignores stale 401 after token rotation, and token-change subscription stops after cleanup.

Use a small test fake:

```ts
class FakeEventSource extends EventTarget {
  closed = false;
	onerror: ((event: Event) => void) | null = null;
  close() { this.closed = true; }
  emit(type: string, data: string) {
    const event = new Event(type);
    Object.defineProperty(event, "data", { value: data });
    this.dispatchEvent(event);
  }
}
```

- [ ] **Step 2: Run tests to verify module is missing**

```bash
pnpm test
```

Expected: FAIL because `lib/domain-stream.ts` does not exist.

- [ ] **Step 3: Add `zoneId` to canonical FE domain type**

Preserve fields from FE `origin/main` and insert:

```ts
zoneId: string;
```

Do not remove `path`, `managed`, or `cloudflareStatus`.

- [ ] **Step 4: Implement parsers and keys**

Define keys once:

```ts
export const domainKeys = {
  all: ["domains"] as const,
  detail: (id: string) => ["domains", id] as const,
  logs: (id: string) => ["domains", id, "logs"] as const,
  metrics: (id: string) => ["domains", id, "metrics"] as const,
};
```

At JSON trust boundary, require current backend fields including `zoneId`; normalize optional newer fields:

```ts
function parseDomain(value: unknown): Domain | undefined {
  if (!isRecord(value)) return undefined;
  if (
    typeof value.id !== "string" ||
    typeof value.hostname !== "string" ||
    typeof value.originUrl !== "string" ||
    typeof value.zoneId !== "string" ||
    typeof value.status !== "string" ||
    !DOMAIN_STATUSES.has(value.status as DomainStatus) ||
    !Number.isInteger(value.metricsPort) ||
    !Number.isInteger(value.pid) ||
    !Number.isInteger(value.restartCount) ||
    typeof value.createdAt !== "string" ||
    typeof value.updatedAt !== "string"
  ) return undefined;

	if (
		!isOptionalString(value.lastError) ||
		!isOptionalString(value.cloudflareTunnelId) ||
		!isOptionalString(value.dnsRecordId) ||
		(value.path !== undefined && typeof value.path !== "string") ||
		(value.managed !== undefined && typeof value.managed !== "boolean") ||
		(value.cloudflareStatus !== undefined && typeof value.cloudflareStatus !== "string")
	) return undefined;

	return {
    ...(value as unknown as Domain),
    path: typeof value.path === "string" ? value.path : "",
    managed: typeof value.managed === "boolean" ? value.managed : true,
    cloudflareStatus: typeof value.cloudflareStatus === "string" ? value.cloudflareStatus : "",
  };
}
```

Also validate optional string fields and reject objects with wrong optional-field types. Convert `items: null` to an empty array to match current Gin response behavior.

- [ ] **Step 5: Implement race-safe cache updates**

For a complete unfiltered domain snapshot, capture previous list IDs, cancel exact affected queries, set list and returned details, then remove absent previous details:

```ts
export async function applyDomainSnapshot(queryClient: QueryClient, snapshot: DomainSnapshot): Promise<void> {
  const previous = queryClient.getQueryData<Domain[]>(domainKeys.all) ?? [];
  await Promise.all([
    queryClient.cancelQueries({ queryKey: domainKeys.all, exact: true }),
    ...previous.map((item) => queryClient.cancelQueries({ queryKey: domainKeys.detail(item.id), exact: true })),
    ...snapshot.items.map((item) => queryClient.cancelQueries({ queryKey: domainKeys.detail(item.id), exact: true })),
  ]);
  queryClient.setQueryData(domainKeys.all, snapshot.items);
  const currentIDs = new Set(snapshot.items.map((item) => item.id));
  for (const item of snapshot.items) queryClient.setQueryData(domainKeys.detail(item.id), item);
  for (const item of previous) {
    if (!currentIDs.has(item.id)) queryClient.removeQueries({ queryKey: domainKeys.detail(item.id), exact: true });
  }
}
```

Implement log and metric updates with exact-key cancellation before `setQueryData`.

- [ ] **Step 6: Implement source binders and session helpers**

Define a testable structural source type:

```ts
export interface EventSourceLike extends EventTarget {
  close(): void;
  onerror: ((event: Event) => void) | null;
}
```

`bindDomainListSource` listens only to `domains`; `bindDomainDetailSource` listens to `logs` and `metrics`, intentionally ignores `metrics-error`, and both return cleanup functions that remove listeners and close source. Keep `createSessionChecker` generation/abort behavior from tests. Token change uses exact event name `session-token-changed`.

Implement binders with named listener references so cleanup removes exact functions:

```ts
export function bindDomainListSource(source: EventSourceLike, queryClient: QueryClient): () => void {
	const onDomains = (event: Event) => {
		const snapshot = parseDomainSnapshot((event as MessageEvent<string>).data);
		if (snapshot) void applyDomainSnapshot(queryClient, snapshot);
	};
	source.addEventListener("domains", onDomains);
	return () => {
		source.removeEventListener("domains", onDomains);
		source.close();
	};
}

export function bindDomainDetailSource(
	source: EventSourceLike,
	queryClient: QueryClient,
	id: string,
): () => void {
	const onLogs = (event: Event) => {
		const lines = parseLogSnapshot((event as MessageEvent<string>).data);
		if (lines) void applyLogSnapshot(queryClient, id, lines);
	};
	const onMetrics = (event: Event) => {
		const text = parseMetricSnapshot((event as MessageEvent<string>).data);
		if (text !== undefined) void applyMetricSnapshot(queryClient, id, text);
	};
	source.addEventListener("logs", onLogs);
	source.addEventListener("metrics", onMetrics);
	return () => {
		source.removeEventListener("logs", onLogs);
		source.removeEventListener("metrics", onMetrics);
		source.close();
	};
}
```

- [ ] **Step 7: Run stream-core tests**

```bash
pnpm test
```

Expected: PASS.

- [ ] **Step 8: Commit FE stream core**

```bash
git add lib/api.ts lib/domain-stream.ts lib/domain-stream.test.ts
git commit -m "feat: synchronize domain caches from SSE"
```

### Task 7: Mount Domain and Detail Streams and Remove Polling

**Files:**
- Create: `components/domain-stream-provider.tsx`
- Create: `hooks/use-domain-detail-stream.ts`
- Modify: `hooks/use-domains.ts`
- Modify: `app/(authenticated)/layout.tsx`
- Modify: `components/domain-detail.tsx`
- Modify: `lib/api.ts`

**Interfaces:**
- Consumes: stream binders/session helpers and `domainKeys` from Task 6.
- Produces: one global `EventSource('/api/domains/stream')` and one mounted detail `EventSource('/api/domains/<encoded-id>/stream')`.
- Produces: password change dispatches `session-token-changed` after replacement cookie succeeds.

- [ ] **Step 1: Write failing token-rotation behavior test**

Add to `lib/domain-stream.test.ts`:

```ts
test("session token signal reconnects every active subscriber", () => {
  const target = new EventTarget();
  let globalRestarts = 0;
  let detailRestarts = 0;
  const stopGlobal = subscribeToSessionTokenChanges(target, () => globalRestarts++);
  const stopDetail = subscribeToSessionTokenChanges(target, () => detailRestarts++);

  notifySessionTokenChanged(target);
  assert.equal(globalRestarts, 1);
  assert.equal(detailRestarts, 1);
  stopGlobal();
  stopDetail();
});
```

Run:

```bash
pnpm test
```

Expected: FAIL until token helpers are fully exported/implemented by Task 6; if already PASS, retain as regression and continue.

- [ ] **Step 2: Add global stream provider**

Create a client provider. On transport error, validate `/api/session`; redirect only on 401. Temporary network errors keep native reconnect active:

```tsx
"use client";

import { useEffect } from "react";
import { useQueryClient } from "@tanstack/react-query";

import {
  bindDomainListSource,
  createSessionChecker,
  subscribeToSessionTokenChanges,
} from "@/lib/domain-stream";

export function DomainStreamProvider({ children }: { children: React.ReactNode }) {
  const queryClient = useQueryClient();

  useEffect(() => {
    let source: EventSource | undefined;
    let unbind = () => {};
    let disposed = false;
    const checker = createSessionChecker(
      (signal) => fetch("/api/session", { cache: "no-store", signal }).then((response) => response.status),
      () => {
        if (disposed) return;
        source?.close();
        queryClient.clear();
        window.location.assign("/login");
      },
    );
    const connect = () => {
      checker.reset();
      unbind();
      if (disposed) return;
      source = new EventSource("/api/domains/stream");
      unbind = bindDomainListSource(source, queryClient);
      source.onerror = checker.run;
    };
    connect();
    const unsubscribe = subscribeToSessionTokenChanges(window, connect);
    return () => {
      disposed = true;
      checker.reset();
      unsubscribe();
      unbind();
    };
  }, [queryClient]);

  return children;
}
```

Ensure binder cleanup and reconnect are idempotent so `unbind()` may run before first source exists.

- [ ] **Step 3: Add detail stream hook**

Create `hooks/use-domain-detail-stream.ts` with the same session policy. URL-encode ID:

```ts
export function useDomainDetailStream(id: string) {
  const queryClient = useQueryClient();
  useEffect(() => {
    if (!id) return;
    let source: EventSource | undefined;
    let unbind = () => {};
    let disposed = false;
    const checker = createSessionChecker(
      (signal) => fetch("/api/session", { cache: "no-store", signal }).then((response) => response.status),
      () => {
        if (disposed) return;
        source?.close();
        queryClient.clear();
        window.location.assign("/login");
      },
    );
    const connect = () => {
      checker.reset();
      unbind();
      if (disposed) return;
      source = new EventSource(`/api/domains/${encodeURIComponent(id)}/stream`);
      unbind = bindDomainDetailSource(source, queryClient, id);
      source.onerror = checker.run;
    };
    connect();
    const unsubscribe = subscribeToSessionTokenChanges(window, connect);
    return () => {
      disposed = true;
      checker.reset();
      unsubscribe();
      unbind();
    };
  }, [id, queryClient]);
}
```

- [ ] **Step 4: Mount providers/hooks**

Wrap authenticated layout content:

```tsx
<DomainStreamProvider>
  <SidebarProvider>{/* existing sidebar and inset */}</SidebarProvider>
</DomainStreamProvider>
```

Call hook unconditionally near top of `DomainDetail`:

```ts
useDomainDetailStream(id);
```

This keeps one detail stream across Logs/Metrics tab switches and closes it when detail page unmounts or ID changes.

- [ ] **Step 5: Remove recurring polling, retain one-shot REST bootstrap**

Import `domainKeys` from `@/lib/domain-stream`. Delete all interval constants and every `refetchInterval`. Keep existing `queryFn` and `enabled` values so first load and non-SSE fallback remain available.

- [ ] **Step 6: Reconnect streams after password rotation**

Change `changePassword` in `lib/api.ts`:

```ts
export async function changePassword(input: { currentPassword: string; newPassword: string }): Promise<void> {
  await unwrap(axios.put<void>("/api/session/password", input));
  if (typeof window !== "undefined") notifySessionTokenChanged(window);
}
```

Import `notifySessionTokenChanged` from `@/lib/domain-stream`.

- [ ] **Step 7: Verify FE stream integration**

```bash
pnpm test
pnpm lint
pnpm build
```

Expected: PASS; no `refetchInterval` remains in `hooks/use-domains.ts`.

- [ ] **Step 8: Commit mounted streams**

```bash
git add components/domain-stream-provider.tsx hooks/use-domain-detail-stream.ts hooks/use-domains.ts app/'(authenticated)'/layout.tsx components/domain-detail.tsx lib/api.ts lib/domain-stream.test.ts
git commit -m "refactor: replace domain polling with SSE"
```

### Task 8: Add Cloudflare Zone Selection to Domain Creation

**Files:**
- Modify: `lib/api.ts`
- Modify: `lib/api-config.ts`
- Modify: `lib/api-config.test.ts`
- Modify: `hooks/use-domains.ts`
- Modify: `components/create-domain-dialog.tsx`

**Interfaces:**
- Consumes: existing generic `/api` client and create form from FE `origin/main`.
- Produces: `CloudflareZone`, `listCloudflareZones()`, `zoneKeys.all`, and `useCloudflareZones(enabled)`.
- Produces: `CreateDomainInput` containing `hostname`, `originUrl`, `path`, and `zoneId`.

- [ ] **Step 1: Write failing payload/default-selection tests**

Update `lib/api-config.test.ts`:

```ts
import {
  createDomainPayload,
  selectedZoneID,
  updateOriginPayload,
} from "./api-config.ts";

test("domain payload preserves path and includes selected zone", () => {
  assert.deepEqual(
    createDomainPayload("app.example.com", "http://localhost:3001", "/api/.*", "zone-1"),
    {
      hostname: "app.example.com",
      originUrl: "http://localhost:3001",
      path: "/api/.*",
      zoneId: "zone-1",
    },
  );
});

test("zone selection keeps a valid choice and defaults to first zone", () => {
  const zones = [{ id: "zone-1" }, { id: "zone-2" }];
  assert.equal(selectedZoneID("zone-2", zones), "zone-2");
  assert.equal(selectedZoneID("missing", zones), "zone-1");
  assert.equal(selectedZoneID("", []), "");
});
```

- [ ] **Step 2: Run config tests**

```bash
pnpm test
```

Expected: FAIL because create payload lacks `zoneId` and `selectedZoneID` is undefined.

- [ ] **Step 3: Extend API config and zone client**

Update input/helper while preserving existing path field:

```ts
export interface CreateDomainInput {
  hostname: string;
  originUrl: string;
  path: string;
  zoneId: string;
}

export function createDomainPayload(
  hostname: string,
  originUrl: string,
  path: string,
  zoneId: string,
): CreateDomainInput {
  return { hostname, originUrl, path, zoneId };
}

export function selectedZoneID(current: string, zones: ReadonlyArray<{ id: string }>): string {
  return zones.some((zone) => zone.id === current) ? current : zones[0]?.id ?? "";
}
```

Add API contract:

```ts
export interface CloudflareZone {
  id: string;
  name: string;
  status: string;
}

interface ListCloudflareZonesResponse {
  items: CloudflareZone[];
}

export async function listCloudflareZones(): Promise<CloudflareZone[]> {
  const response = await unwrap(client.get<ListCloudflareZonesResponse>("/cloudflare/zones"));
  return response.items;
}
```

- [ ] **Step 4: Add zones query**

In hooks:

```ts
export const zoneKeys = { all: ["cloudflare-zones"] as const };

export function useCloudflareZones(enabled: boolean) {
  return useQuery({
    queryKey: zoneKeys.all,
    queryFn: listCloudflareZones,
    enabled,
  });
}
```

- [ ] **Step 5: Add accessible native zone selector**

In create dialog:

- Call `useCloudflareZones(open)`.
- Track `zoneId` state.
- On zone result change, run `setZoneId((current) => selectedZoneID(current, zones ?? []))`.
- Submit `createDomainPayload(hostname, originUrl, path, zoneId)`.
- Reset `zoneId` after success.

Add field:

```tsx
<div className="grid gap-2">
  <Label htmlFor="zone-id">Cloudflare zone</Label>
  <select
    id="zone-id"
    className="border-input bg-background h-9 w-full rounded-md border px-3 text-sm disabled:cursor-not-allowed disabled:opacity-50"
    value={zoneId}
    onChange={(event) => setZoneId(event.target.value)}
    disabled={zonesQuery.isPending || zonesQuery.isError || !zonesQuery.data?.length}
    required
  >
    {(zonesQuery.data ?? []).map((zone) => (
      <option key={zone.id} value={zone.id}>{zone.name}</option>
    ))}
  </select>
  {zonesQuery.isPending && <p className="text-xs text-muted-foreground">Loading Cloudflare zones…</p>}
  {zonesQuery.isError && <p className="text-xs text-destructive">Failed to load Cloudflare zones.</p>}
  {zonesQuery.data?.length === 0 && <p className="text-xs text-destructive">No active Cloudflare zones are available.</p>}
</div>
```

Use exact state synchronization:

```tsx
const zonesQuery = useCloudflareZones(open);

useEffect(() => {
	setZoneId((current) => selectedZoneID(current, zonesQuery.data ?? []));
}, [zonesQuery.data]);
```

Import `useEffect` beside `useState`.

Disable submit at trust boundary:

```tsx
disabled={createDomain.isPending || !zoneId || zonesQuery.isPending || zonesQuery.isError}
```

Do not add a select dependency.

- [ ] **Step 6: Verify multi-zone frontend**

```bash
pnpm test
pnpm lint
pnpm build
```

Expected: PASS. Build confirms current Next.js 16 client/server boundaries and `RouteContext` types.

- [ ] **Step 7: Commit zone creation flow**

```bash
git add lib/api.ts lib/api-config.ts lib/api-config.test.ts hooks/use-domains.ts components/create-domain-dialog.tsx
git commit -m "feat: select Cloudflare zone for domains"
```

### Task 9: Cross-Repository Verification

**Files:**
- Verify only; modify only if command output exposes a defect in planned files.

**Interfaces:**
- Consumes: backend endpoint and frontend native `EventSource` integration from Tasks 1-8.
- Produces: evidence both repositories compile and all committed tests pass.

- [ ] **Step 1: Verify backend from clean worktree**

```bash
git status --short
go test ./...
go build ./...
```

Expected: clean status and both Go commands PASS.

- [ ] **Step 2: Verify frontend from clean worktree**

```bash
git status --short
pnpm test
pnpm lint
pnpm build
```

Expected: clean status and all FE commands PASS.

- [ ] **Step 3: Run manual same-origin smoke test**

Start backend and frontend using existing project commands. Log in, then verify through browser network/UI:

1. Exactly one open `/api/domains/stream` request under authenticated layout.
2. Exactly one `/api/domains/<id>/stream` request while detail page is mounted.
3. Creating a domain sends selected `zoneId`; table updates without five-second polling.
4. New complete log lines appear without `/logs` polling.
5. Metrics update about every four seconds without `/metrics` polling.
6. Leaving detail page closes detail stream.
7. Password change reconnects both streams and keeps current session.
8. Temporary backend metrics failure shows no destructive cache reset; logs continue.

- [ ] **Step 4: Record verification in final handoff**

Report exact command results, commit IDs in each repository, and any manual smoke step not run. Do not claim unrun checks passed.
