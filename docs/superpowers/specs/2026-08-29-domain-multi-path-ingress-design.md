# Domain Multi-Path Ingress Design

**Date:** 2026-08-29

## Goal

Allow one managed domain to route different URL path prefixes to different
local HTTP services while preserving the current lifecycle of one Cloudflare
tunnel, one DNS record, and one `cloudflared` process per domain.

Example:

```text
https://app.example.com/api/users -> http://localhost:8080/users
https://app.example.com/admin     -> http://localhost:3000/admin
https://app.example.com/anything  -> http://localhost:5173/anything
```

This feature selects an origin by path. Each non-root route may optionally
remove its matched prefix before proxying to the local service.

## Verified Cloudflare Behavior

The design was checked against the current Cloudflare documentation and the
project's pinned `github.com/cloudflare/cloudflare-go/v6 v6.10.0` SDK.

- Ingress rules may match `hostname`, `path`, or both.
- Rules are evaluated from top to bottom and the first match wins.
- A rule without `path` matches every path for its hostname.
- The final ingress rule must be a catch-all rule; this backend keeps
  `service: http_status:404` as that rule.
- `cloudflared` represents `path` as a regular expression. The pinned Go SDK
  exposes it as
  `TunnelCloudflaredConfigurationUpdateParamsConfigIngress.Path`.
- Cloudflare Tunnel ingress has no field that strips or rewrites the path; it
  only selects the destination service. Cloudflare URL Rewrite Rules are a
  separate zone ruleset capability with separate permissions and lifecycle.
- Remotely managed tunnels store ingress configuration in Cloudflare and can
  be updated through the existing tunnel configuration API.

Primary references retrieved through Context7:

- Cloudflare Tunnel configuration: traffic matching and catch-all rules.
- Cloudflare Tunnel remote API: `PUT /accounts/{account_id}/cfd_tunnel/{tunnel_id}/configurations`.
- `cloudflared` ingress API: `UnvalidatedIngressRule{Hostname, Path, Service}`
  and first-match behavior.

## Scope

### Included

- Persist an ordered set of path routes for each domain.
- Route each path prefix to one local HTTP or HTTPS origin URL.
- Optionally strip a non-root route prefix before forwarding to its origin.
- Keep a default `/` route so every domain has deterministic fallback
  behavior before the global 404 catch-all.
- Create a domain with one or more routes.
- Replace all routes for an existing domain in one authenticated API call.
- Return routes in domain create, get, list, and SSE snapshots.
- Preserve existing domains through a non-destructive migration.
- Keep existing tunnel, DNS, process, logs, metrics, stop, restart, and delete
  behavior unchanged.

### Excluded

- Arbitrary URL rewrites, prefix replacement, or query rewriting beyond the
  boolean matched-prefix removal described here.
- Managing Cloudflare Transform/URL Rewrite Rules.
- Header-, method-, query-, or cookie-based routing.
- Wildcard hostnames or multiple hostnames per tunnel.
- TCP, SSH, RDP, Unix socket, or other non-HTTP origins.
- Per-route Cloudflare `originRequest` settings.
- Route health checks or automatic failover.
- Importing routes edited outside this backend.
- Partial route mutations or drag-and-drop priority management.

## Architecture

Keep the current dependency flow:

```text
HTTP route/handler
  -> domain service
     -> domain repository
     -> Cloudflare client -> Cloudflare API
     -> local ingress proxy
```

The domain remains the aggregate root. Domain routes do not own Cloudflare
tunnels, DNS records, processes, logs, or metrics. They only describe ingress
rules belonging to the parent domain. Extend `internal/pkg/repo/domain` with
aggregate route reads and writes so that this repository owns the SQLite
transactions spanning `domains` and `domain_routes`. Do not put Bun queries or
transaction handling in the service.

Add `internal/pkg/ingressproxy`, a loopback-only `net/http` reverse proxy. A
Cloudflare rule whose route has `stripPrefix: true` targets this proxy instead
of targeting the origin directly. The proxy repeats the same hostname and
longest-prefix match, removes the prefix, and forwards to the persisted origin.
Routes without stripping continue to target their origins directly through
`cloudflared`, avoiding an unnecessary extra hop.

## Path Contract

The public API accepts literal path prefixes, not arbitrary regular
expressions. This keeps ordering and overlap behavior understandable to users.

Normalization and validation:

1. Trim surrounding whitespace.
2. Require the path to start with `/`.
3. Reject query strings, fragments, and control characters. Treat every
  accepted path character literally; raw regular expression semantics are
  never exposed.
4. Remove trailing `/` except for the root path `/`.
5. Require exactly one `/` route per domain.
6. Reject duplicate normalized paths.
7. Limit a domain to 50 routes and each path to 256 bytes.

Before sending a path to Cloudflare, convert it to an anchored Go/RE2-safe
regular expression:

```text
/      -> no path field (hostname default route)
/api   -> ^/api(/.*)?$
/admin -> ^/admin(/.*)?$
```

`regexp.QuoteMeta` must be used when building the expression. Go's regexp
engine does not support non-capturing groups, so use `(/.*)?` rather than
`(?:/.*)?`. Specific routes
are sorted by descending normalized path length, then lexically for stable
ties. The `/` route is emitted after every specific route. The global
`http_status:404` catch-all remains last.

This means `/api/v2` is checked before `/api`, regardless of request array or
database order. `/apiary` does not match `/api`.

Each route also accepts `stripPrefix`, defaulting to `false`:

- It must be `false` for `/`; stripping the root prefix has no useful effect.
- For path `/api`, `stripPrefix: false` forwards `/api/users` unchanged.
- For path `/api`, `stripPrefix: true` forwards `/api` and `/api/` as `/`, and
  forwards `/api/users` as `/users`.
- The raw query string is preserved unchanged.
- Matching and stripping use Go's decoded `URL.Path` and segment boundary
  rules; the proxy must never use a plain `strings.TrimPrefix` without first
  proving the route match.

## Origin Contract

Each route contains one `originUrl`:

- Require an absolute URL with `http` or `https` scheme and a non-empty host.
- Reject credentials, query strings, and fragments.
- Require an empty URL path or `/`; reject a non-root origin path because this
  feature does not define path joining or rewriting semantics. Normalize a
  trailing `/` away before persistence.
- The expected origin shape is `http://localhost:<port>` or a container DNS
  equivalent.
- Keep the current deployment semantics: `localhost` resolves from the
  machine or container running the backend and `cloudflared`.
- Preserve the incoming `Host` header. For stripped requests, set
  `X-Forwarded-Prefix` to the removed canonical prefix and use the standard
  reverse-proxy `X-Forwarded-*` headers.

## Data Model

Add a model free of HTTP framework concerns:

```go
type DomainRoute struct {
	ID        string    `bun:"id,pk" json:"id"`
	DomainID  string    `bun:"domain_id,notnull" json:"-"`
	Path      string    `bun:"path,notnull" json:"path"`
	OriginURL string    `bun:"origin_url,notnull" json:"originUrl"`
  StripPrefix bool    `bun:"strip_prefix,notnull,default:false" json:"stripPrefix"`
	CreatedAt time.Time `bun:"created_at,notnull" json:"createdAt"`
	UpdatedAt time.Time `bun:"updated_at,notnull" json:"updatedAt"`
}
```

Add `Routes []DomainRoute` to the serialized `model.Domain`; mark it
`bun:"-"` so repositories load it explicitly. Keep `Domain.OriginURL` and the
`domains.origin_url` column during this release as a compatibility projection
of the `/` route. This avoids an unnecessary domain-table rebuild and keeps
older clients readable while `routes` becomes the canonical write contract.

Migration `00005_create_domain_routes.sql`:

- Enable SQLite foreign key enforcement on every application database
  connection; declaring a foreign key alone is insufficient in SQLite.
- Create `domain_routes` with a foreign key to `domains(id)` and
  `ON DELETE CASCADE`.
- Add `strip_prefix INTEGER NOT NULL DEFAULT 0`.
- Add `UNIQUE(domain_id, path)`.
- Add an index on `(domain_id, path)`.
- Backfill exactly one `/` route for every existing domain from
  `domains.origin_url`, with `strip_prefix = 0`.
- Preserve all existing domain rows and remote Cloudflare resources.
- Down migration drops only `domain_routes`.

## HTTP API

All endpoints remain protected by the existing JWT/cookie middleware.

### Create domain

`POST /api/domains`

```json
{
  "hostname": "app.example.com",
  "zoneId": "023e105f4ecef8ad9ca31a8372d0c353",
  "routes": [
    {"path": "/api", "originUrl": "http://localhost:8080", "stripPrefix": true},
    {"path": "/admin", "originUrl": "http://localhost:3000", "stripPrefix": false},
    {"path": "/", "originUrl": "http://localhost:5173", "stripPrefix": false}
  ]
}
```

`routes` is required and must satisfy the path contract. The old
`originUrl`-only create shape returns `400 Bad Request`; avoiding two writable
shapes prevents ambiguous state.

### Replace domain routes

`PUT /api/domains/:id/routes`

```json
{
  "routes": [
    {"path": "/api/v2", "originUrl": "http://localhost:8081", "stripPrefix": true},
    {"path": "/api", "originUrl": "http://localhost:8080", "stripPrefix": true},
    {"path": "/", "originUrl": "http://localhost:5173", "stripPrefix": false}
  ]
}
```

Replace the full set rather than expose individual add/delete/reorder calls.
The service can validate conflicts and push one complete Cloudflare
configuration, preventing temporarily invalid rule sets.

`PUT /api/domains/:id` is retired from the documented contract. It may remain
registered for one compatibility release and translate `originUrl` into a
replacement of only the `/` route while preserving specific routes. Mark it
deprecated in `README.md` and tests.

Domain responses retain legacy `originUrl` as the `/` route projection and add
an ordered `routes` array.

## Cloudflare Adapter

Replace the scalar adapter contract:

```go
PutIngressConfig(ctx, tunnelID, hostname, originURL string) error
```

with:

```go
type IngressRule struct {
	Path    string
	Service string
}

PutIngressConfig(ctx context.Context, tunnelID, hostname string, rules []IngressRule) error
```

The adapter maps each specific rule to SDK fields `Hostname`, `Path`, and
`Service`, maps `/` to a hostname rule without `Path`, and always appends the
catch-all `{Service: "http_status:404"}`.

Keep path normalization, validation, and ordering in the service. The adapter
should still defensively reject an empty rule set so it cannot publish only a
404 due to a caller bug.

The service builds each adapter rule as follows:

- `stripPrefix: false`: `Service` is the route's `originUrl`.
- `stripPrefix: true`: `Service` is the loopback URL exposed by
  `ingressproxy`, such as `http://127.0.0.1:20080`.

## Local Ingress Proxy

Add a small infrastructure component under `internal/pkg/ingressproxy` using
only `net/http`, `net/http/httputil`, `net/url`, and immutable snapshots.

Contract:

```go
type Proxy interface {
	URL() string
	Start() error
	Shutdown(ctx context.Context) error
  PrepareDomain(domainID, hostname string, oldRoutes, newRoutes []Route) error
  CommitDomain(domainID, hostname string, routes []Route) error
  RollbackDomain(domainID, hostname string, routes []Route) error
	RemoveDomain(domainID string)
}
```

Requirements:

- Listen on `INGRESS_PROXY_ADDR`, default `127.0.0.1:20080`.
- Reject non-loopback bind addresses during configuration loading because this
  internal data plane has no public authentication.
- Keep one process-wide server and an atomically swapped immutable routing
  snapshot; do not start one listener per domain.
- Normalize request hosts by removing a port, lowercasing ASCII, and removing
  one trailing dot.
- Match only routes marked `stripPrefix`, using longest canonical prefix with
  a segment boundary.
- Return `404` for unknown host/path and `502` for an unreachable origin
  without exposing internal error details.
- Preserve query strings, request bodies, streaming responses, and WebSocket
  upgrades through `httputil.ReverseProxy`.
- Remove the exact matched prefix and ensure the resulting path starts with
  `/`; `/api` and `/api/` both become `/`.
- Clear `URL.RawPath` after rewriting so the forwarded escaped path agrees with
  the rewritten decoded path; test encoded path segments and boundary cases.
- Reject route origins that point back to `INGRESS_PROXY_ADDR` to prevent a
  self-proxy loop.
- Do not register management endpoints on this listener.

`PrepareDomain` installs a transition snapshot before the Cloudflare update:
new stripped routes override old routes with the same path, while old-only
stripped routes remain temporarily available. This prevents requests still
using the old remote configuration from receiving a local 404 when a stripped
route is removed or changed to direct forwarding. `CommitDomain` installs only
the persisted new routes after Cloudflare and SQLite succeed;
`RollbackDomain` restores only the old routes after a failed update.

The application lifecycle starts the proxy listener before domain reconcile
and shuts it down with the API server. `Reconcile` loads persisted routes into
the proxy snapshot before spawning active `cloudflared` processes.

## Create Flow

1. Handler binds hostname, zone ID, and routes.
2. Service validates/normalizes hostname, paths, origins, route count, and
   duplicates.
3. Service validates the selected active Cloudflare zone and hostname
   ownership.
4. Service creates the tunnel.
5. Service installs the normalized route snapshot in the local proxy.
6. Service writes the complete ordered ingress configuration, targeting the
  proxy only for routes with prefix stripping enabled.
7. Service creates the DNS record.
8. Service encrypts the token and allocates a metrics port.
9. In one SQLite transaction, persist the domain plus all routes; mirror the
   `/` route into `domains.origin_url`.
10. Service starts the existing single `cloudflared` process.
11. Existing DNS/tunnel compensation runs and the proxy snapshot is removed if
   persistence fails.

## Update Flow and Consistency

Route replacement touches Cloudflare and SQLite, so no true cross-system
transaction exists. Use this compensation sequence:

1. Validate and normalize the complete replacement set.
2. Acquire an in-process keyed lock for the domain.
3. Under that lock, load the latest domain and its current routes.
4. Prepare a transition proxy snapshot containing new stripped routes plus
  old-only stripped routes before changing Cloudflare.
5. Push the new complete ingress configuration to Cloudflare.
6. Replace local rows and update the legacy default origin in one SQLite
   transaction.
7. If local persistence fails, best-effort restore the old Cloudflare ingress
   and return `500`.
8. Roll back to the old proxy snapshot whenever Cloudflare fails or persistence
  fails and remote rollback succeeds. If remote rollback fails, retain the
  transition snapshot so either observed remote configuration remains
  routable and log the drift.
9. Commit the new-only proxy snapshot after persistence succeeds.
10. Publish one domain SSE notification only after persistence succeeds.

Use the same keyed lock around domain deletion and the deprecated root-route
update so those mutations cannot interleave with route replacement. If remote
rollback fails, log the drift without including credentials and retain the
local failure response.

Do not restart `cloudflared` after a remote configuration update. If deployment
testing finds that the pinned runtime does not adopt remote updates promptly,
add an explicit restart as a separately tested operational change rather than
assuming it in this feature.

## Read and Delete Flows

- `GetDomain` loads one domain and its ordered routes.
- `ListDomains` batch-loads routes for all returned domain IDs to avoid N+1
  queries, then attaches them in memory.
- Domain list SSE reuses `ListDomains`, so snapshots automatically include
  routes.
- Detail SSE does not need a new event because route mutations already trigger
  the domain update signal used by list snapshots.
- Deleting a domain holds the keyed mutation lock, removes its proxy snapshot,
  and deletes route rows through the foreign key cascade; remote DNS/tunnel
  cleanup remains unchanged.
- Reconcile loads all persisted routes into one proxy snapshot before starting
  active tunnel processes. Backfilled routes have stripping disabled, so their
  existing remote Cloudflare configurations remain valid.
- Reconcile, stop, restart, logs, and metrics remain domain/process concerns
  and do not need route reads.

## Error Handling

- Invalid path, origin, duplicate, missing `/`, or too many routes: `400`.
- `stripPrefix: true` on `/`: `400`.
- Missing domain: `404`.
- Internal proxy bind/start failure: application startup fails.
- Cloudflare ingress update failure: `502`; do not mutate SQLite.
- SQLite failure after Cloudflare success: `500` after best-effort remote
  rollback.
- Never return raw Cloudflare SDK errors or credentials in HTTP responses.

## Concurrency and External Drift

The keyed lock protects concurrent mutations inside one backend process.
Current SQLite configuration already limits the connection pool to one
connection. Migration work must additionally enable and verify
`PRAGMA foreign_keys=ON`. Multi-replica coordination and optimistic versions
are excluded because the current SSE fan-out and process supervision are also
process-local.

Cloudflare configuration edited manually can drift from SQLite. For this
release, the backend remains source of truth and overwrites the full ingress
configuration on the next successful route replacement. Import/reconciliation
of externally edited ingress rules requires a separate design.

## Testing

Use the Go standard testing package and existing local HTTP/fake patterns.

### Migration and repository

- Preserve existing domain rows and backfill one `/` route each.
- Enforce unique `(domain_id, path)` and cascade deletion.
- Backfill `strip_prefix = 0`.
- Create domain and routes atomically.
- Replace routes and legacy default origin atomically.
- Batch-load routes without N+1 queries.

### Validation and ordering

- Normalize trailing slashes.
- Reject missing root, duplicate normalized prefixes, malformed paths,
  unsupported origins, and more than 50 routes.
- Reject stripping `/` and verify omitted `stripPrefix` defaults to `false`.
- Order `/api/v2` before `/api`, and `/` last.
- Prove `/api` compiles to a Go-compatible, boundary-safe regex that does not match
  `/apiary`.

### Cloudflare adapter

- Assert the exact `PUT` request path and JSON ingress array.
- Assert hostname/path/service fields and deterministic order.
- Assert `/` omits `path`.
- Assert stripping rules target the internal proxy and non-stripping rules
  target their origins directly.
- Assert the final rule is always `http_status:404`.

### Service

- Create publishes and persists all routes.
- Route replacement does not recreate DNS, tunnel, token, metrics port, or
  process.
- Cloudflare failure leaves persisted routes unchanged.
- Persistence failure attempts Cloudflare rollback.
- Two concurrent replacements for one domain cannot interleave.
- Successful replacement emits one subscriber notification.

### Local ingress proxy

- Match normalized hostname and longest segment-boundary prefix.
- Strip `/api` correctly for `/api`, `/api/`, and `/api/users`; reject
  `/apiary`.
- Preserve query, body, Host, streaming, and `X-Forwarded-*` behavior.
- Set `X-Forwarded-Prefix` only when stripping.
- Atomically replace and remove domain snapshots under concurrent requests.
- Keep old-only stripped routes available in a transition snapshot until the
  remote update commits; ensure new same-path routes take precedence.
- Reject public bind addresses and return safe `404`/`502` responses.

### HTTP and streaming

- Create rejects the old scalar payload and returns routes.
- Route replacement validates input and maps `400`/`404`/`502`/`500`.
- Get, list, and domain-list SSE snapshots include ordered routes.
- Every new route requires authentication.

## Rollout

1. Back up the SQLite database.
2. Deploy migration `00005`; unlike `00004`, it preserves domain rows.
3. Verify every existing domain has one `/` route matching its former
   `origin_url`.
4. Deploy the backend and update clients to send `routes` on create.
5. Create a test domain with stripped `/api`, preserved `/admin`, and `/`, then
  verify all public and upstream paths.
6. Replace the routes of the test domain and verify remote changes without a
   process restart.
7. Monitor backend errors and `cloudflared` logs before enabling the feature
   broadly.

## Documentation Updates

Update `README.md` with:

- New create and route replacement payloads.
- Prefix matching and first-match behavior.
- `stripPrefix` semantics, default value, examples, and
  `X-Forwarded-Prefix` behavior.
- `INGRESS_PROXY_ADDR` and its loopback-only security constraint.
- Local/container meaning of `localhost`.
- The required `/` fallback route and 50-route limit.
- Deprecation notice for `PUT /api/domains/:id`.
- Migration and verification instructions.
