# Domain Detail SSE and Multi-Zone Frontend Design

**Date:** 2026-07-29
**Status:** Approved

## Goal

Replace frontend polling with backend Server-Sent Events (SSE) for domain lists, domain logs, and domain metrics. Align domain creation with the backend multi-zone contract by loading Cloudflare zones and submitting `zoneId`.

## Scope

This change adds:

- `GET /api/domains/:id/stream` for log snapshots and metric snapshots.
- Event-driven log snapshots when complete log lines change.
- Metric snapshots immediately and every four seconds.
- Frontend SSE synchronization for domain lists, domain details, logs, and metrics.
- Cloudflare zone loading and selection during domain creation.
- SSE-safe forwarding through the existing Next.js same-origin API proxy.

This change preserves:

- `GET /api/domains/stream` and its existing full-snapshot contract.
- Existing JSON logs and Prometheus-text metrics endpoints for compatibility.
- Existing Bearer and cookie authentication behavior.
- TanStack Query as the frontend cache read interface.

This change does not add:

- Delta log events, event IDs, replay, or `Last-Event-ID` handling.
- Cross-process or cross-replica pub/sub.
- Parsed or charted Prometheus metrics.
- A new third-party dependency.

## Architecture

Backend flow remains within the existing domain module:

```text
GET /api/domains/:id/stream
  -> JWT middleware
  -> domain SSE handler
  -> domain service
       -> domain repository for existence checks
       -> log buffer subscription and snapshots
       -> cloudflared metrics HTTP endpoint
```

HTTP and SSE formatting stay in `internal/application/api/route/domain`. Domain existence checks, log access, and metric retrieval stay in `internal/services/domain`. Log buffering and non-blocking change notification stay in `internal/pkg/logbuf`.

Frontend uses two native `EventSource` connections:

```text
authenticated layout
  -> /api/domains/stream
  -> TanStack Query domain list/detail caches

domain detail page
  -> /api/domains/:id/stream
  -> TanStack Query logs/metrics caches
```

Components keep reading query caches. They do not parse SSE directly.

## Backend Domain Detail Stream

### Request

```text
GET /api/domains/:id/stream
```

The endpoint accepts no query parameters. It uses the existing JWT middleware and token precedence. Before committing SSE headers, the handler verifies that the domain exists:

- Missing domain returns HTTP `404` JSON.
- Authentication failure returns HTTP `401` JSON.
- Other initial setup failures return HTTP `500` JSON.

### Response Headers

Successful responses include:

```text
Content-Type: text/event-stream
Cache-Control: no-cache
X-Accel-Buffering: no
```

### Log Events

Each log event contains the current snapshot of at most 500 complete lines:

```text
event: logs
data: {"items":["line 1","line 2"]}
```

Stream sends one `logs` event immediately. Each newly completed line triggers another full snapshot. Partial trailing lines remain hidden until a newline completes them, matching the existing logs endpoint.

`logbuf.Buffer` owns subscriptions because it already detects complete lines. Each subscriber receives a capacity-one notification channel. Publishing is non-blocking; rapid writes coalesce into one pending refresh. Signals contain no payload. Handler retrieves the latest copied snapshot after each signal.

A domain with no live log buffer sends `{"items":[]}`. If no buffer exists at stream setup, the service still provides a stable subscription path for that domain so a later process start can trigger snapshots without reconnecting.

> `ponytail:` Log events send full 500-line snapshots and use process-local notifications. Add sequenced delta events plus shared pub/sub only when payload size or multiple backend replicas require them.

### Metric Events

Each metric event wraps the existing raw Prometheus exposition text:

```text
event: metrics
data: {"text":"# HELP ...\n"}
```

Stream requests metrics immediately and every four seconds. Metric retrieval accepts request context and uses a bounded HTTP client timeout shorter than the four-second interval. Request cancellation aborts an in-flight scrape.

A scrape failure after stream startup does not close the connection. It emits:

```text
event: metrics-error
data: {"message":"metrics unavailable"}
```

The next scheduled interval retries. Internal network, process, and response details remain server-side.

> `ponytail:` Metric transport sends raw text snapshots every four seconds. Add structured metric parsing or adaptive intervals only when UI needs charts or scrape load becomes material.

### Heartbeat and Lifecycle

Server sends an SSE comment every 15 seconds:

```text
: heartbeat
```

Handler performs these steps:

1. Authenticate and resolve domain.
2. Establish log subscription.
3. Commit SSE headers.
4. Send current logs snapshot.
5. Attempt and send current metrics snapshot or `metrics-error`.
6. Wait for log notification, four-second metric interval, heartbeat interval, or request cancellation.
7. On cancellation or socket write failure, cancel subscription and stop all tickers and requests.

No event contains `id:` or `retry:`. Native `EventSource` reconnects automatically. Every reconnect receives fresh snapshots, so replay is unnecessary.

## Existing Domain List Stream

`GET /api/domains/stream` remains unchanged:

```text
event: domains
data: {"items":[...],"nextCursor":"..."}
```

Frontend keeps one connection mounted for the authenticated layout. A `domains` event:

1. Replaces the domain-list query cache.
2. Replaces each returned domain-detail cache.
3. Removes stale detail cache entries for domains absent from the unfiltered snapshot.

The global stream uses the unfiltered endpoint without pagination, making absence meaningful. Filtered or paginated views remain derived client-side or fetched explicitly; they must not use a partial snapshot to delete detail caches.

Malformed payloads are ignored and logged without replacing valid cache data.

## Frontend Domain Detail Synchronization

A focused client hook owns `EventSource('/api/domains/:id/stream')` while a domain detail screen is mounted.

- `logs` parses `{ items: string[] }` and writes the existing logs query cache.
- `metrics` parses `{ text: string }` and writes the existing metrics query cache.
- `metrics-error` preserves previous metrics and exposes no destructive cache update.
- Cleanup closes `EventSource`.
- Changing `id` closes the old stream before opening the new one.

Existing `useDomains`, `useDomain`, `useLogs`, and `useMetrics` remain cache access hooks, but their polling intervals are removed. Initial REST queries may remain as fallback/bootstrap reads where needed; recurring polling must be removed.

One detail stream carries logs and metrics to avoid two authenticated browser connections and duplicate lifecycle handling.

## Authentication and Reconnection

Browser connects to same-origin `/api/...` routes. The Next.js proxy reads `tunnel-manager-session` and supplies the backend Bearer token. Native `EventSource` never receives or exposes JWT values.

The proxy must:

- Stream the upstream response body without buffering.
- Forward `Content-Type`, `Cache-Control`, and `X-Accel-Buffering`.
- Pass the incoming request abort signal to upstream `fetch`.
- Preserve non-stream JSON errors before connection establishment.

Network errors keep existing cache data and rely on native reconnect. Because `EventSource.onerror` does not expose HTTP status, frontend checks the existing session endpoint after transport failure. An invalid session closes streams and navigates to `/login`; a valid session leaves reconnect enabled.

Password change replaces the session JWT. Successful password change triggers a shared stream-generation invalidation so mounted domain and detail streams close and reconnect with the replacement token.

## Multi-Zone Domain Creation

Frontend aligns types and requests with the backend contract:

```json
{
  "hostname": "app.example.com",
  "originUrl": "http://app:8080",
  "zoneId": "zone-id"
}
```

`Domain` gains `zoneId`. Frontend adds the Cloudflare zone shape:

```json
{
  "id": "zone-id",
  "name": "example.com",
  "status": "active"
}
```

When create dialog opens:

1. Fetch `GET /api/cloudflare/zones` through the existing API client.
2. Show loading and failure states.
3. Select the first returned zone by default.
4. Let user choose another zone using existing/native form controls.
5. Submit `hostname`, `originUrl`, and selected `zoneId`.

When no zones exist, creation is disabled with a clear message. Successful creation relies on the domain SSE snapshot to refresh the table; mutation invalidation may remain as a harmless immediate fallback.

Backend route registration for `GET /api/cloudflare/zones` must pass both required arguments to `middleware.JWTAuth`, including current config, so the backend compiles and applies normal authentication behavior.

## Error Handling

### Backend

- Missing domain before stream: HTTP `404` JSON.
- Invalid auth: HTTP `401` JSON.
- Initial internal setup failure: HTTP `500` JSON.
- Log snapshot failure after startup: generic `error` event, then close.
- Metric scrape failure: `metrics-error`, keep stream open and retry.
- Client disconnect: cancel log subscription, tickers, and active scrape.
- Slow subscribers never block process output writes.

### Frontend

- Invalid SSE payload: ignore event and preserve cache.
- Temporary network failure: preserve cache and reconnect.
- Expired session: close streams and navigate to login.
- Zone fetch failure: keep dialog open, show error, disable submission.
- Empty zone list: explain requirement and disable submission.

## Files and Responsibilities

Expected backend changes:

- `internal/application/api/route/domain/route.go`
  - Register detail stream and fix zones middleware wiring.
- `internal/application/api/route/domain/stream.go` or a focused detail-stream file
  - Manage SSE encoding and lifecycle.
- `internal/services/domain/service.go`
  - Expose context-aware metrics and log subscription operations.
- `internal/services/domain` metric/log implementation files
  - Retrieve snapshots and metrics without HTTP-layer concerns.
- `internal/pkg/logbuf/logbuf.go`
  - Add non-blocking complete-line change subscriptions.

Expected frontend changes:

- `lib/api.ts`
  - Add `zoneId`, zone types/API, and updated create request.
- `hooks/use-domains.ts`
  - Remove recurring polling and expose cache keys needed by stream synchronization.
- One focused domain stream client/provider
  - Own global `EventSource` and domain cache synchronization.
- One focused detail stream hook
  - Own detail `EventSource` and logs/metrics cache synchronization.
- `app/(authenticated)/layout.tsx`
  - Mount global stream provider once.
- `components/domain-detail.tsx`
  - Mount detail stream hook for current domain.
- `components/create-domain-dialog.tsx`
  - Load and select Cloudflare zone.
- `lib/server/backend-core.ts` and `lib/server/backend.ts`
  - Propagate cancellation and SSE response headers.
- Password change flow
  - Reconnect streams after token replacement.

Use existing UI components where available. Do not add a component library or SSE package.

## Testing

### Backend

Use Go standard `testing`. Use `httptest.NewServer` plus a real HTTP client for streaming tests.

Cover:

- Detail stream authentication and missing-domain responses.
- Correct SSE headers.
- Immediate logs and metrics snapshots.
- Complete log line triggers a refreshed 500-line-bounded snapshot.
- Partial line does not publish until completed.
- Rapid log notifications coalesce without blocking writer.
- Metrics repeat after four seconds.
- Metrics timeout emits `metrics-error` and later retries.
- Heartbeat emission.
- Client cancellation removes subscription and stops work.
- Existing REST logs/metrics behavior remains compatible.
- Zones route compiles and remains authenticated.

### Frontend

Use the existing test stack and mock native `EventSource` without adding a dependency.

Cover:

- Domain event parser and list/detail cache updates.
- Deleted-domain cache cleanup only from complete unfiltered snapshots.
- Logs and metrics event parsing and cache updates.
- Malformed events preserve prior cache.
- Stream cleanup on unmount and domain ID change.
- Password change reconnects streams.
- Proxy forwards SSE headers and abort signal.
- Zone loading, default selection, empty/error state, and create payload.

### Verification

Backend:

```text
go test ./...
go build ./...
```

Frontend:

```text
pnpm test
pnpm lint
pnpm build
```

## Acceptance Criteria

- Domain list and detail state update without recurring frontend polling.
- Logs update when complete lines arrive and reconnect with a current bounded snapshot.
- Metrics update immediately and every four seconds through SSE.
- Temporary metric failures do not terminate log streaming.
- Browser disconnect releases backend subscriptions, tickers, and HTTP requests.
- Existing REST logs and metrics endpoints remain compatible.
- Domain creation requires and submits a valid Cloudflare `zoneId`.
- Same-origin proxy preserves streaming and authentication behavior.
- Password changes reconnect active streams with the replacement session token.
- No new third-party dependency is added.
