# Domain Multi-Path Ingress Implementation Plan

> Follow the repository architecture conventions and implement each task test-first. Keep one commit per task when practical.

**Goal:** Let one managed hostname route multiple path prefixes to different local HTTP services through its existing Cloudflare tunnel.

**Architecture:** Keep one domain as one tunnel/DNS/process aggregate. Persist child route rows, compile literal path prefixes into ordered Cloudflare ingress regexes, and use one loopback reverse proxy to remove prefixes only for routes that request it.

**Tech Stack:** Go 1.26.1, Gin 1.12.0, Fx 1.24.0, Bun 1.2.18, SQLite, Goose SQL migrations, `cloudflare-go/v6` v6.10.0, Go standard `testing`.

**Design:** `docs/superpowers/specs/2026-08-29-domain-multi-path-ingress-design.md`

## Global Constraints

- Keep `route -> service -> repository/infrastructure` dependency direction.
- Keep one tunnel, DNS record, token, metrics port, log buffer, and process per domain.
- Treat API paths as literal prefixes; never accept raw user regex.
- Require exactly one `/` route and append global `http_status:404` last.
- Support optional `stripPrefix`; default it to `false` and reject it for `/`.
- Do not manage Cloudflare URL Rewrite/Transform Rules; stripping happens in a
  loopback-only local reverse proxy.
- Preserve existing domain rows in migration `00005`.
- Keep `domains.origin_url` as a temporary read-compatible projection of `/`.
- Replace all routes in one operation; do not add partial route mutation APIs.
- Never expose Cloudflare SDK errors or secrets in HTTP responses.
- Add no dependency or test framework.

## File Map

- `internal/model/domain.go`: attach non-Bun `Routes` collection.
- `internal/model/domain_route.go`: persisted route model.
- `internal/pkg/request/domain/create.go`: replace scalar origin with routes.
- `internal/pkg/request/domain/routes.go`: route input and replace payload.
- `migrations/00005_create_domain_routes.sql`: table, index, and legacy backfill.
- `migrations/migrations_test.go`: preservation/backfill/schema tests.
- `internal/pkg/repo/domain/*`: route persistence, batch reads, and
  transactional aggregate create/update helpers.
- `internal/pkg/sqlite/sqlite.go`: enable SQLite foreign key enforcement.
- `internal/pkg/config/config.go`: validate the loopback ingress proxy address.
- `internal/pkg/ingressproxy/*`: immutable route snapshots and prefix-stripping reverse proxy.
- `internal/pkg/ingressproxy/fx.go`: proxy construction and lifecycle wiring.
- `internal/pkg/cloudflare/type.go`: ingress rule slice contract.
- `internal/pkg/cloudflare/client.go`: map routes to SDK ingress entries.
- `internal/pkg/cloudflare/client_test.go`: exact tunnel configuration payload tests.
- `internal/services/domain/routes.go`: normalization, validation, regex compilation, and ordering.
- `internal/services/domain/create.go`: create aggregate with multiple routes.
- `internal/services/domain/update.go`: full replacement plus compensation.
- `internal/services/domain/get.go`: attach routes to one domain.
- `internal/services/domain/list.go`: batch-attach routes.
- `internal/services/domain/service.go`: proxy dependency, interface, errors, and keyed mutation lock.
- `internal/services/domain/reconcile.go`: hydrate proxy routes before process reconcile.
- `internal/services/domain/service_test.go`: create/update/rollback/concurrency tests.
- `internal/application/api/route/domain/create.go`: new create payload.
- `internal/application/api/route/domain/routes.go`: route replacement handler.
- `internal/application/api/route/domain/update.go`: deprecated root-route compatibility.
- `internal/application/api/route/domain/route.go`: register authenticated route endpoint.
- `internal/application/api/route/domain/route_test.go`: contract/auth/error tests.
- `internal/application/api/route/domain/stream_test.go`: routes in list snapshots.
- `README.md`: API, semantics, limits, migration, and rollout docs.
- `.env.example`: loopback ingress proxy address.

---

### Task 1: Add Route Schema and Models

**Files:**
- Create: `migrations/00005_create_domain_routes.sql`
- Modify: `migrations/migrations_test.go`
- Create: `internal/model/domain_route.go`
- Modify: `internal/model/domain.go`
- Create: `internal/pkg/request/domain/routes.go`
- Modify: `internal/pkg/request/domain/create.go`

- [ ] Add a failing migration test that applies migrations `00001` and `00004`, inserts a current domain, applies `00005`, and verifies the domain survives with one `/` route copied from `origin_url`.
- [ ] Add checks for `UNIQUE(domain_id, path)`, the `(domain_id, path)` index, and `ON DELETE CASCADE` behavior.
- [ ] Implement `00005` with `domain_routes(id, domain_id, path, origin_url, created_at, updated_at)` and an `INSERT ... SELECT` backfill using deterministic IDs such as `legacy-<domain-id>`.
- [ ] Add `strip_prefix INTEGER NOT NULL DEFAULT 0`; verify existing domains are backfilled with stripping disabled.
- [ ] Add `model.DomainRoute.StripPrefix`; add `Routes []DomainRoute` to `model.Domain` with `bun:"-"`.
- [ ] Add `RouteInput{Path, OriginURL, StripPrefix}` and `ReplaceRoutesRequest{Routes}` with required binding tags; change create request to require `routes` and remove writable scalar `originUrl`.
- [ ] Run `go test ./migrations ./internal/model ./internal/pkg/request/domain` and `gofmt` changed Go files.

**Acceptance:** Existing domain rows survive migration and each receives exactly one `/` route.

---

### Task 2: Add Domain Route Persistence

**Files:**
- Create: `internal/pkg/repo/domain/routes.go`
- Create: `internal/pkg/repo/domain/routes_test.go`
- Modify: `internal/pkg/repo/domain/repo.go`
- Modify: `internal/pkg/repo/domain/create.go`
- Modify: `internal/pkg/repo/domain/update.go`
- Modify: `internal/pkg/sqlite/sqlite.go`
- Create or modify: `internal/pkg/sqlite/sqlite_test.go`

- [ ] Extend the domain repository with methods to list one domain's routes, list routes for many domain IDs in one query, create a domain aggregate, and replace all routes.
- [ ] Add repository tests for stable ordering, batch grouping, duplicate rejection, replacement rollback, and cascade deletion.
- [ ] Implement aggregate operations that create a domain plus routes in one transaction and replace routes plus `domains.origin_url` in one transaction. Keep transaction ownership in the repository and do not expose Bun transaction types to the service.
- [ ] Keep existing domain `Update`/`UpdateBulk` methods for process status updates.
- [ ] Enable `PRAGMA foreign_keys=ON` when opening the application's SQLite connection and prove it is active in a focused test.
- [ ] Run focused repository tests, then `go test ./internal/pkg/repo/...`.

**Acceptance:** No partially persisted domain/route aggregate is possible in SQLite, and list reads avoid N+1 queries.

---

### Task 3: Add the Prefix-Stripping Proxy

**Files:**
- Create: `internal/pkg/ingressproxy/type.go`
- Create: `internal/pkg/ingressproxy/proxy.go`
- Create: `internal/pkg/ingressproxy/proxy_test.go`
- Create: `internal/pkg/ingressproxy/fx.go`
- Modify: `internal/pkg/config/config.go`
- Modify: `internal/pkg/config/config_test.go`
- Modify: `internal/application/fx.go`
- Modify: `internal/pkg/lifecycle/lifecycle.go`
- Modify: `.env.example`

- [ ] Define a narrow proxy interface for URL discovery, start/shutdown, prepare/commit/rollback of one domain snapshot, and removal.
- [ ] Table-test loopback address validation; default `INGRESS_PROXY_ADDR` to `127.0.0.1:20080` and reject wildcard, public, hostname, Unix socket, and missing-port values.
- [ ] Build one `httputil.ReverseProxy` data-plane server backed by atomically swapped immutable route snapshots; do not add management HTTP endpoints.
- [ ] Test hostname normalization and longest segment-boundary prefix matching, including `/api/v2`, `/api`, `/apiary`, host ports, casing, and trailing dots.
- [ ] Test `/api` and `/api/` become `/`, `/api/users` becomes `/users`, and query strings remain unchanged.
- [ ] Clear `URL.RawPath` after stripping and test encoded segment/boundary cases.
- [ ] Preserve the incoming Host header, request body, streaming behavior, WebSocket upgrades, and standard `X-Forwarded-*`; set `X-Forwarded-Prefix` to the removed canonical prefix.
- [ ] Reject an origin URL that resolves to the configured proxy endpoint to prevent a local forwarding loop.
- [ ] Test transition snapshots: new stripped routes override same-path old routes, while removed or newly direct old stripped routes remain until commit; rollback restores old-only state.
- [ ] Return safe `404` for unknown routes and safe `502` for origin failures.
- [ ] Test concurrent requests while replacing/removing snapshots with `go test -race` where supported.
- [ ] Wire the proxy through Fx; start it before domain reconcile and shut it down with a bounded lifecycle context.
- [ ] Run `go test ./internal/pkg/ingressproxy ./internal/pkg/config ./internal/pkg/lifecycle -v`.

**Acceptance:** The internal proxy strips only an already matched canonical prefix and is unreachable outside loopback.

---

### Task 4: Generalize Cloudflare Ingress Updates

**Files:**
- Modify: `internal/pkg/cloudflare/type.go`
- Modify: `internal/pkg/cloudflare/client.go`
- Modify: `internal/pkg/cloudflare/client_test.go`

- [ ] Add a failing local HTTP server test asserting an exact configuration request containing two hostname/path/service rules, one hostname default rule without `path`, and final `http_status:404`.
- [ ] Replace scalar `PutIngressConfig` arguments with `hostname` plus `[]IngressRule`.
- [ ] Map non-root rules to SDK `Hostname`, `Path`, and `Service`; map root to `Hostname` and `Service`; append catch-all internally.
- [ ] Prove service URLs may point either directly to the route origin or to the loopback ingress proxy; keep that policy in the domain service, not the adapter.
- [ ] Reject an empty application rule set before calling Cloudflare.
- [ ] Keep existing account scoping and error wrapping; do not leak response bodies or credentials.
- [ ] Run `go test ./internal/pkg/cloudflare -v`.

**Acceptance:** The adapter emits a deterministic valid Cloudflare ingress payload with catch-all last.

---

### Task 5: Validate and Compile Route Prefixes

**Files:**
- Create: `internal/services/domain/routes.go`
- Create: `internal/services/domain/routes_test.go`
- Modify: `internal/services/domain/service.go`

- [ ] Table-test path normalization: root, trailing slash, nested prefix, whitespace, duplicates after normalization, query/fragment/control characters, missing root, and over 50 routes.
- [ ] Table-test origin URL validation for HTTP/HTTPS, missing host, credentials, query, fragment, non-root origin paths, and unsupported schemes; normalize a root trailing slash away.
- [ ] Test omitted `stripPrefix` defaults to false and reject `stripPrefix: true` for `/`.
- [ ] Implement pure normalization returning canonical model routes and Cloudflare rules.
- [ ] Compile prefixes with `regexp.QuoteMeta` into the Go-compatible `^<prefix>(/.*)?$`; emit `/` with no path expression and explicitly avoid unsupported non-capturing groups.
- [ ] Sort by descending prefix length, lexical tie-break, and root last.
- [ ] Add domain errors that handlers can safely map to `400` and upstream ingress errors to `502`.
- [ ] Add a per-domain keyed lock to the service; ensure lock entries are released after mutation.
- [ ] Build Cloudflare rule services from normalized routes: direct origin when stripping is false, proxy URL when stripping is true.
- [ ] Run `go test ./internal/services/domain -run 'Test.*Route' -v`.

**Acceptance:** `/api/v2` precedes `/api`; `/api` matches `/api` and `/api/x` but not `/apiary`, and every emitted expression compiles with Go's `regexp` package.

---

### Task 6: Create Domains with Multiple Routes

**Files:**
- Modify: `internal/services/domain/service.go`
- Modify: `internal/services/domain/create.go`
- Modify: `internal/services/domain/service_test.go`
- Modify: `internal/application/api/route/domain/create.go`
- Modify: `internal/application/api/route/domain/route_test.go`

- [ ] Change `CreateDomain` to accept a shaped create command or route inputs rather than adding more scalar arguments.
- [ ] Extend service fakes to capture the complete ordered Cloudflare ingress set.
- [ ] Test validation occurs before tunnel creation.
- [ ] Test successful create installs the proxy snapshot before publishing ingress, targets the proxy only for stripped routes, persists routes atomically, and mirrors `/` into legacy `OriginURL`.
- [ ] Test existing tunnel/DNS rollback still runs and the provisional proxy snapshot is removed when create fails.
- [ ] Update handler binding and prove old `{originUrl}` payload returns `400`, while the new `routes` payload returns `201` with ordered routes.
- [ ] Run focused service and route tests.

**Acceptance:** One create request produces one tunnel with multiple path rules and one local process.

---

### Task 7: Replace Routes with Compensation

**Files:**
- Modify: `internal/services/domain/service.go`
- Modify: `internal/services/domain/update.go`
- Modify: `internal/services/domain/service_test.go`
- Create: `internal/application/api/route/domain/routes.go`
- Modify: `internal/application/api/route/domain/update.go`
- Modify: `internal/application/api/route/domain/route.go`
- Modify: `internal/application/api/route/domain/route_test.go`

- [ ] Add `ReplaceRoutes(ctx, domainID, inputs)` to the service contract.
- [ ] Test the service locks by domain ID, loads the latest routes, prepares a transition proxy snapshot, pushes the complete config, persists once, then commits the new-only snapshot.
- [ ] Test Cloudflare failure leaves SQLite untouched, restores the old proxy snapshot, and maps to `502`.
- [ ] Test persistence failure invokes best-effort Cloudflare rollback; restore the old proxy snapshot when rollback succeeds.
- [ ] Test failed remote rollback retains the transition proxy snapshot so either possible active remote config remains routable, logs drift safely, and returns `500`.
- [ ] Test concurrent replacements for the same domain cannot interleave Cloudflare and DB steps.
- [ ] Use the same keyed lock in `DeleteDomain` and the deprecated root-route update; test delete cannot interleave with route replacement.
- [ ] Remove the domain proxy snapshot during delete and restore it if the local delete fails while the domain still exists.
- [ ] Publish exactly one domain update after successful local persistence.
- [ ] Register `PUT /api/domains/:id/routes` under existing auth middleware and implement safe error mapping.
- [ ] Preserve `PUT /api/domains/:id` for one deprecated compatibility release by replacing only `/` while retaining specific routes.
- [ ] Run focused service/route tests.

**Acceptance:** Route replacement is complete-set, serialized, compensating, and does not recreate or restart domain resources.

---

### Task 8: Hydrate Routes in Reads and Reconcile

**Files:**
- Modify: `internal/services/domain/get.go`
- Modify: `internal/services/domain/list.go`
- Modify: `internal/services/domain/service.go`
- Create or modify: `internal/services/domain/reconcile.go`
- Modify: `internal/services/domain/service_test.go`
- Modify: `internal/application/api/route/domain/route_test.go`
- Modify: `internal/application/api/route/domain/stream_test.go`

- [ ] Make `GetDomain` attach ordered routes from one repository read.
- [ ] Make `ListDomains` batch-load all routes for the returned page and attach them by domain ID.
- [ ] Add tests that empty pages do not query routes and populated pages do one batch route query.
- [ ] Verify get/list JSON includes `routes` and compatible `originUrl`.
- [ ] Verify initial and mutation-driven `domains` SSE snapshots include ordered routes.
- [ ] During startup reconcile, batch-load persisted routes for all domains and atomically hydrate proxy snapshots before spawning active tunnel processes.
- [ ] Verify stopped domains are also hydrated so a later restart needs no proxy rebuild; backfilled non-stripping routes produce no proxy match.
- [ ] Fail reconcile safely if persisted route data is invalid instead of spawning an unroutable active tunnel.
- [ ] Confirm detail log/metrics SSE remains unchanged.
- [ ] Run all domain service and domain route tests.

**Acceptance:** Every domain representation consistently exposes routes without N+1 queries.

---

### Task 9: Document, Format, and Verify

**Files:**
- Modify: `README.md`
- Modify: `.env.example`
- Modify: any changed Go files requiring formatting

- [ ] Document create/replace payloads, `stripPrefix` default and behavior, prefix boundaries, preserved query strings, `X-Forwarded-Prefix`, root fallback, route limit, local/container origin meaning, and deprecated update endpoint.
- [ ] Document `INGRESS_PROXY_ADDR`, its loopback-only restriction, and startup failure behavior.
- [ ] Document migration `00005` as non-destructive and provide post-migration verification behavior.
- [ ] Run `gofmt` on all changed Go files.
- [ ] Run `go test ./...`.
- [ ] Run `go build ./...`.
- [ ] Review the final diff for accidental credentials, architecture violations, and unrelated changes.
- [ ] Manually verify against a real test tunnel: stripped `/api`, stripped nested `/api/v2`, preserved `/admin`, `/`, `/apiary`, query strings, WebSocket/streaming if used, and route replacement while `cloudflared` stays running.

**Acceptance:** Tests and build pass, docs match behavior, and real Cloudflare routing confirms remote update propagation.

## Expected API Example

After implementation, a domain response includes:

```json
{
  "id": "domain-id",
  "hostname": "app.example.com",
  "originUrl": "http://localhost:5173",
  "routes": [
    {"id": "route-api", "path": "/api", "originUrl": "http://localhost:8080", "stripPrefix": true},
    {"id": "route-root", "path": "/", "originUrl": "http://localhost:5173", "stripPrefix": false}
  ]
}
```

The service sends Cloudflare rules in this order:

```text
hostname=app.example.com path=^/api(/.*)?$   service=http://127.0.0.1:20080
hostname=app.example.com path=<omitted>       service=http://localhost:5173
hostname=<omitted>        path=<omitted>       service=http_status:404
```
