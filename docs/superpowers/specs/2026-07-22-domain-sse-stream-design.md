# Domain SSE Stream and Cookie Authentication Design

**Date:** 2026-07-22
**Status:** Approved

## Goal

Provide a continuously updated domain list to the frontend through Server-Sent Events (SSE). Native browser `EventSource` must work through an `HttpOnly` JWT cookie, while existing API clients may continue using `Authorization: Bearer`.

## Scope

This change adds:

- `GET /api/domains/stream` for domain snapshots over SSE.
- A process-local domain change broadcaster.
- JWT authentication from either Bearer header or an `HttpOnly` cookie.
- Cookie issuance during login and cookie removal during logout.
- Origin validation for unsafe cookie-authenticated requests.
- Configuration and documentation for secure cookie behavior.

This change does not add:

- Delta events or an event replay log.
- `Last-Event-ID` recovery.
- Cross-instance event distribution.
- A new external dependency.

## Architecture

Runtime flow remains consistent with repository conventions:

```text
GET /api/domains/stream
  -> JWT middleware
  -> domain SSE handler
  -> domain service subscription and list query
  -> domain repository
  -> SQLite
```

Domain mutations retain the existing flow:

```text
route -> handler -> domain service -> repository/infrastructure
                                      -> publish change signal after success
```

HTTP and SSE formatting stay in `internal/application/api/route/domain`. Subscription ownership and mutation notifications stay in `internal/services/domain`. Database queries remain in `internal/pkg/repo/domain`.

## Endpoint Contract

### Request

```text
GET /api/domains/stream?status=<status>&hostname=<hostname>&pageSize=<n>&cursor=<cursor>
```

The stream reuses `domainrequest.ListDomainRequest`, including current filtering and pagination semantics. Authentication accepts:

1. A valid `Authorization: Bearer <token>` header.
2. When no Authorization header is present, a valid JWT cookie.

If an Authorization header is present but malformed or invalid, the request is rejected. Middleware must not fall back to cookie authentication in that case.

### Response

Successful response headers include:

```text
Content-Type: text/event-stream
Cache-Control: no-cache
X-Accel-Buffering: no
```

Each domain snapshot uses event name `domains` and JSON data matching the existing list response:

```text
event: domains
data: {"items":[...],"nextCursor":"..."}
```

Server sends an SSE comment heartbeat every 15 seconds:

```text
: heartbeat
```

Heartbeat contains no application state.

### Stream Lifecycle

Handler performs these steps in order:

1. Bind and validate list query parameters.
2. Subscribe to domain change notifications.
3. Query the initial domain snapshot.
4. Set SSE response headers and send initial `domains` event.
5. Wait for a change notification, heartbeat interval, or request cancellation.
6. On change, query and send a fresh filtered snapshot.
7. On client disconnect, unsubscribe and return.

Subscribing before the first query prevents a mutation between initial list loading and stream setup from being lost. Buffered notifications may cause one redundant snapshot, which is acceptable.

## Domain Change Broadcaster

`domainService` owns a process-local subscriber set protected by a dedicated mutex, separate from log-buffer locking. Each subscriber receives a buffered `chan struct{}` with capacity `1`.

The service exposes subscription through its interface. Subscription returns a receive-only notification channel plus an idempotent cancellation function. Cancellation removes the subscriber and closes no shared publisher state.

Publishing is non-blocking:

- Send one empty signal to each subscriber.
- If a subscriber buffer already contains a signal, skip that send.
- Never allow a slow or disconnected SSE client to block a domain mutation.

Signals contain no domain payload. Handler reloads the complete requested snapshot after each signal. This coalesces rapid mutations and guarantees frontend convergence without implementing delta ordering or replay logic.

All successful domain persistence writes publish through small service-level wrappers around repository `Create`, `Update`, `UpdateBulk`, and `Delete`. This covers:

- Domain creation.
- Origin update.
- Domain deletion.
- Stop and restart operations when persisted state changes.
- Successful persistence of supervisor process events.
- Reconcile persistence when it changes domain status.

Failed persistence writes do not publish. If an outer operation fails after an earlier persistence write succeeded, that write still publishes because it changed frontend-visible state.

> `ponytail:` Broadcaster is process-local and supports one backend process. Add Redis Pub/Sub, database notifications, or another shared event transport before running multiple backend replicas.

## Authentication and Cookie Contract

### Login

Existing login JSON remains backward compatible:

```json
{
  "token": "<jwt>",
  "expiresAt": "<timestamp>"
}
```

Successful login also sets JWT cookie with:

- Name: `tunnel_manager_token`.
- Value: generated JWT.
- `HttpOnly=true`.
- `SameSite=Strict`.
- `Path=/api`.
- `Secure` from `AUTH_COOKIE_SECURE`.
- `Expires` aligned with JWT expiration.
- Positive `Max-Age` aligned with remaining JWT lifetime.

`AUTH_COOKIE_SECURE` defaults to `true`. Local HTTP development must explicitly set it to `false`.

Successful password change sets the replacement JWT in the same cookie, because token-version rotation immediately invalidates the previous cookie.

### Middleware Token Selection

JWT middleware follows strict precedence:

1. If `Authorization` exists, require exactly one valid Bearer token and authenticate it.
2. Otherwise, read `tunnel_manager_token` and authenticate it.
3. Otherwise, return `401 {"error":"unauthorized"}`.

Middleware records whether authentication came from Bearer or cookie for CSRF enforcement. Error responses never expose token details.

### CSRF Protection

Safe methods `GET`, `HEAD`, and `OPTIONS` need no CSRF check.

For `POST`, `PUT`, `PATCH`, and `DELETE`:

- Bearer-authenticated requests bypass cookie CSRF checks because browsers do not attach custom Authorization headers cross-origin without explicit client code and CORS approval.
- Cookie-authenticated requests require an `Origin` header equal to configured `CORS_ALLOWED_ORIGIN`.
- Missing, malformed, unset, or mismatched origin returns `403`.

Deployments using cookie authentication must set `CORS_ALLOWED_ORIGIN` to the frontend's exact public origin, including scheme and port where applicable. Wildcard origins are not accepted with credentials.

Public login and logout routes preserve non-browser compatibility: if an `Origin` header is present, it must match `CORS_ALLOWED_ORIGIN`; requests without `Origin` remain allowed. Browser cross-origin requests supply `Origin` and are rejected before credentials are created or cleared.

### Logout

Add `POST /api/auth/logout`. It clears `tunnel_manager_token` using the same `Path`, `Secure`, `HttpOnly`, and `SameSite` attributes, with an expired timestamp and negative `Max-Age`.

Logout succeeds idempotently even when JWT is absent or expired, allowing clients to clear stale cookies. If `Origin` is present, it must match `CORS_ALLOWED_ORIGIN`; missing `Origin` remains valid for non-browser clients. Bearer tokens remain stateless; clients discard them locally. Password changes continue invalidating issued JWTs through token versioning.

## CORS

Current CORS middleware remains allowlist-based and adds:

```text
Access-Control-Allow-Credentials: true
```

only when `CORS_ALLOWED_ORIGIN` is configured. Allowed headers continue including `Authorization` and `Content-Type`. No wildcard origin is used.

Same-origin frontend deployments do not require CORS but still use `CORS_ALLOWED_ORIGIN` as the explicit CSRF origin value.

## Error Handling

Before response headers are committed:

- Invalid query returns HTTP `400` JSON.
- Initial list failure returns HTTP `500` JSON.
- Authentication failure returns HTTP `401` JSON.
- Cookie CSRF failure returns HTTP `403` JSON.

After stream starts:

- Snapshot reload failure sends one named `error` SSE event with generic data `{"message":"stream unavailable"}` and closes the connection.
- Write failure or request cancellation closes the handler without another response.
- `EventSource` reconnect behavior obtains a new initial snapshot.

Internal errors may be logged, but database, JWT, and infrastructure details must not be sent through SSE.

## Files and Responsibilities

Expected changes stay within current module boundaries:

- `internal/application/api/route/domain/route.go`
  - Register authenticated stream route.
- `internal/application/api/route/domain/stream.go`
  - Bind query and manage SSE lifecycle.
- `internal/services/domain/service.go`
  - Expose subscription contract and hold subscribers.
- Existing domain service mutation files
  - Publish after successful state changes.
- `internal/application/api/route/auth/login.go`
  - Set JWT cookie while preserving JSON response.
- `internal/application/api/route/auth/change_password.go`
  - Replace JWT cookie after token-version rotation.
- `internal/application/api/route/auth/logout.go`
  - Clear JWT cookie.
- `internal/application/api/route/auth/route.go`
  - Register logout route.
- `internal/pkg/middleware/auth.go`
  - Support Bearer and cookie token sources plus CSRF enforcement.
- `internal/pkg/middleware/origin.go`
  - Validate browser origins for public login and logout routes.
- `internal/pkg/middleware/cors.go`
  - Allow credentialed requests for configured exact origin.
- `internal/pkg/config/config.go`, `.env.example`, and `README.md`
  - Add and document `AUTH_COOKIE_SECURE` and cookie-origin requirements.

Shared cookie constants or helpers should remain in the smallest existing package that avoids duplication between auth route and middleware. Do not introduce a new abstraction unless login, logout, and middleware genuinely share it.

## Testing

Use Go's standard `testing` package.

### Authentication Tests

Cover:

- Valid Bearer authentication.
- Valid cookie authentication when header is absent.
- Bearer precedence over cookie.
- Malformed or invalid Bearer rejection without cookie fallback.
- Missing credentials rejection.
- Unsafe Bearer request without Origin acceptance.
- Unsafe cookie request with matching Origin acceptance.
- Missing or mismatched Origin rejection for unsafe cookie requests.
- Login cookie attributes and retained JSON token response.
- Password change replaces the invalidated JWT cookie.
- Logout cookie expiration and idempotent response.
- Login and logout reject a supplied untrusted Origin while retaining clients that send no Origin.

### Broadcaster Tests

Cover:

- Subscriber receives a publish notification.
- Multiple pending publishes coalesce in capacity-one buffer.
- One slow subscriber does not block publishing to others.
- Cancellation removes subscriber and is idempotent.

### SSE Tests

Use `httptest.NewServer` with a real HTTP client rather than `httptest.ResponseRecorder`, because streaming depends on flushing and connection lifecycle support.

Cover:

- Correct SSE headers.
- Immediate initial `domains` event.
- Query filters passed to list operation.
- Mutation notification causes a new snapshot.
- Rapid notifications coalesce without blocking mutation.
- Heartbeat comment is emitted.
- Request cancellation ends subscription.
- Reload error sends generic `error` event and closes stream.

### Verification

Run:

```text
go test ./...
go build ./...
```

All existing authentication and domain behavior must remain compatible for Bearer clients.

## Acceptance Criteria

- Native `EventSource` can connect with same-origin JWT cookie.
- Bearer-authenticated API and stream clients continue working.
- Stream sends current filtered snapshot immediately.
- Successful domain state changes trigger refreshed snapshots.
- Slow clients never block service mutations.
- Disconnected clients are removed without goroutine or subscriber leaks.
- Unsafe cookie-authenticated mutations reject untrusted origins.
- Login sets secure cookie and logout clears it.
- No new third-party dependency is added.
