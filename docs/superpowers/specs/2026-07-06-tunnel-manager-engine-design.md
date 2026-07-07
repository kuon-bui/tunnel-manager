# Tunnel Manager Engine — Design Spec

Date: 2026-07-06
Status: Approved (engine phase; UI and observability stack are separate follow-up specs)

## Context

Domains are currently exposed to the internet via one hand-managed `cloudflared`
container reading a single `config.yml` with multiple ingress rules (see
`data/cloudflared/config.yml`). Adding a new exposed service today means
manually editing that YAML, restarting the container, and re-running
`cloudflared tunnel route dns` for the new hostname.

This spec covers the **first of three sub-projects** needed to replace that
with a self-service system:

1. **Tunnel manager engine (this spec)** — a Go backend that owns the
   lifecycle of one Cloudflare Tunnel per exposed domain.
2. Web UI (Next.js) to add/remove/inspect domains — future spec, consumes the
   REST API defined here.
3. Observability stack (Prometheus/Grafana/Alertmanager, Loki/ELK/Vector) —
   future spec, consumes the metrics/log hooks defined here.

## Goals

- One Cloudflare Tunnel per domain, fully isolated (its own tunnel ID,
  credentials, ingress config). Deleting one domain never affects another.
- Adding a domain is a single API call: `{hostname, origin_url}` in,
  fully working public HTTPS route out — no manual `cloudflared` CLI steps.
- Changing a domain's origin URL does not require restarting its tunnel
  process.
- Surviving a backend restart/redeploy requires no manual intervention:
  active domains come back on their own.
- A crashing tunnel process does not require manual intervention unless it
  is crash-looping.

## Non-goals (deferred to later specs)

- No web UI in this phase — engine only, driven via REST API (curl/Postman
  during this phase; Next.js frontend is a separate spec).
- No Prometheus/Grafana/Loki deployment in this phase — the engine exposes
  the hooks (metrics proxy endpoint, log files) that a later observability
  spec will wire up.
- No multi-hostname-per-tunnel support — by design, one domain = one tunnel
  (see Architecture Decisions).
- No multi-Cloudflare-account support — one `CLOUDFLARE_API_TOKEN` for the
  whole system.

## Architecture Decisions

### One tunnel per domain

Each domain gets its own Cloudflare Tunnel (own tunnel ID, own credentials),
rather than sharing one tunnel with multiple ingress rules. Trade-off:
more tunnels to manage on the Cloudflare side, but complete isolation —
deleting or misconfiguring one domain cannot break another, and per-domain
metrics/logs are separated from the start.

### Tunnel processes run as subprocesses inside the backend container

The Go backend is a single container. Rather than orchestrating separate
Docker containers per domain (which would need `docker.sock` access and
Docker Engine API integration), the backend spawns and supervises
`cloudflared` as child OS processes within its own container. This means:

- The backend container must sit on the same Docker network as the services
  it exposes (9router, openclaw, etc.) so that spawned `cloudflared`
  processes can reach `http://<service>:<port>` via container DNS.
- No `docker.sock` mount, no Docker SDK dependency.
- Process lifecycle (start/stop/crash/restart) is managed with Go's
  `os/exec` and goroutines, not a container orchestrator.

### Remote-managed tunnel configuration (token-based, not local config files)

Each `cloudflared` subprocess is started as:

```
cloudflared tunnel run --token <TUNNEL_TOKEN> --metrics localhost:<PORT>
```

The ingress rule (hostname → origin service) is pushed to Cloudflare's edge
via the Cloudflare API as **remote tunnel configuration**, not a local
`config.yml`/`credentials.json` pair. This has two direct consequences that
shaped the design:

- No secret files on disk per domain — only the token, encrypted at rest in
  SQLite.
- Changing a domain's origin URL is a single Cloudflare API call (`PUT`
  remote config); `cloudflared` picks up the new config from the cloud
  without a local restart.

### State storage: SQLite

A single SQLite file in a mounted volume holds all domain records. Chosen
over Postgres for this scale (tens of domains, single-writer backend,
home-lab deployment) — no extra service to run and back up.

### Secrets

- `CLOUDFLARE_API_TOKEN` — env var, used only at runtime to call the
  Cloudflare API, never persisted.
- `ENCRYPTION_KEY` — env var, used to AES-GCM-encrypt each domain's
  `tunnel_token` before writing it to SQLite.
- The SQLite file is created with `0600` permissions.
- `tunnel_token` is never included in any API response (always redacted).

## Data Model

Table `domains`:

| Column               | Type      | Notes                                                   |
|----------------------|-----------|----------------------------------------------------------|
| id                   | TEXT (PK) | UUID                                                     |
| hostname             | TEXT      | unique, e.g. `n8n.example.com`                            |
| origin_url           | TEXT      | e.g. `http://n8n:5678`                                   |
| cloudflare_tunnel_id | TEXT      | from Cloudflare API                                      |
| tunnel_token         | BLOB      | AES-GCM encrypted                                        |
| status               | TEXT      | `pending` \| `active` \| `error` \| `stopped`            |
| metrics_port         | INTEGER   | localhost-only port for this tunnel's `--metrics` flag   |
| pid                  | INTEGER   | nullable; current OS process ID when running             |
| restart_count        | INTEGER   | consecutive crash count, resets after 60s of stable run  |
| last_error           | TEXT      | nullable; most recent failure message                    |
| created_at           | TIMESTAMP |                                                            |
| updated_at           | TIMESTAMP |                                                            |

## Domain Lifecycle

### Create (`POST /api/domains`)

1. Validate `hostname` is not already registered.
2. Call Cloudflare API to create a new tunnel → receive `tunnel_id` +
   `tunnel_token`.
3. `PUT` remote tunnel configuration to Cloudflare: one ingress rule
   (`hostname` → `origin_url`) plus the mandatory catch-all
   (`service: http_status:404`).
4. Create a DNS CNAME record: `hostname` → `<tunnel_id>.cfargotunnel.com`.
5. Encrypt `tunnel_token`, insert the SQLite row with `status=pending`.
6. Spawn the `cloudflared` subprocess (see Process Supervisor).
7. On successful start, update `status=active` and `pid`.

**Rollback:** if any of steps 2–4 fails, undo whatever Cloudflare-side state
was already created (delete the tunnel and/or DNS record) and return an
error. No partial row is ever left in SQLite.

### Update (`PUT /api/domains/{id}`)

Only changes `origin_url`. Re-runs step 3 (`PUT` remote config) — no process
restart needed.

### Delete (`DELETE /api/domains/{id}`)

Kill the subprocess (marked as an intentional stop, so the supervisor does
not try to restart it) → delete the DNS record → delete the Cloudflare
tunnel → delete the SQLite row.

### Manual stop/restart

- `POST /api/domains/{id}/stop` — intentional stop; supervisor will not
  auto-restart. `status=stopped`.
- `POST /api/domains/{id}/restart` — clears `restart_count` and
  `last_error`, spawns the subprocess again.

## Process Supervisor

One goroutine per running domain, owning an `exec.Cmd` and calling `Wait()`
in its own goroutine.

- **Crash handling:** if the process exits with a non-zero code and the
  stop was not intentional, back off and retry: 1s, 2s, 4s, 8s, 16s (5
  attempts). After 5 consecutive failures, stop retrying, set
  `status=error` with `last_error` populated, and wait for a manual
  `/restart` call.
- **Backoff reset:** if the process stays up continuously for 60s, the
  `restart_count` resets to 0 (a later crash starts the backoff sequence
  from the beginning again).
- **Intentional stop:** `/stop` and `DELETE` set a flag before killing the
  process so the supervisor treats the exit as expected, not a crash.

### Startup reconciliation

On backend boot, read every row with `status=active` from SQLite and spawn
its subprocess directly using the stored (decrypted) token — no Cloudflare
API calls are needed at this stage, since the tunnel and its token remain
valid across backend restarts.

## REST API Surface

```
POST   /api/domains              create {hostname, origin_url}
GET    /api/domains              list all domains + status
GET    /api/domains/{id}         get one domain's detail
PUT    /api/domains/{id}         update origin_url (hot, no restart)
DELETE /api/domains/{id}         delete domain (cascades to Cloudflare)
POST   /api/domains/{id}/restart manual restart, resets backoff state
POST   /api/domains/{id}/stop    intentional stop, disables auto-restart
GET    /api/domains/{id}/logs    tail of recent log lines
GET    /api/domains/{id}/metrics proxy of this tunnel's Prometheus metrics
```

`tunnel_token` is never present in any response payload.

## Observability Hooks (consumed by a future spec)

- **Logs:** each subprocess's stdout/stderr is captured into (a) an
  in-memory ring buffer (last 500 lines, backs the `/logs` endpoint) and
  (b) appended to `/data/logs/<domain_id>.log`. A future log shipper
  (Vector/Loki) can tail these files directly — no backend changes needed
  when that spec is implemented.
- **Metrics:** each `cloudflared` subprocess runs with
  `--metrics localhost:<metrics_port>` (auto-allocated from a configurable
  port range, default `20500-20999`, loopback-only). The backend exposes
  `GET /api/domains/{id}/metrics` as a reverse proxy to that loopback port,
  avoiding the need to publish dozens of container ports. A future
  Prometheus setup can scrape these proxy endpoints (static targets or via
  a to-be-added `/api/metrics/targets` discovery endpoint) — left to the
  observability spec.

## Tech Stack

- Go, `gin` for HTTP routing.
- `github.com/uptrace/bun` as the SQL layer, `sqlitedialect` backed by
  `modernc.org/sqlite` (pure Go, no cgo — keeps the Docker build simple).
- `github.com/cloudflare/cloudflare-go` for all Cloudflare API calls
  (tunnel create/delete, remote config, DNS record create/delete).
- `os/exec` + goroutines for process supervision — no external process
  manager library.

## Testing Strategy

- Unit tests for the supervisor state machine (mock the spawned command,
  simulate crash/backoff/reset transitions).
- Unit tests for the Cloudflare integration behind an interface (mock the
  SDK), specifically covering rollback when a middle step of tunnel
  creation fails.
- Integration test that spawns a fake shell-script binary in place of the
  real `cloudflared` binary, verifying restart backoff and startup
  reconciliation end-to-end.

## Open Questions for Later Specs

- Exact Prometheus scrape/service-discovery mechanism against the metrics
  proxy endpoints.
- Next.js UI's auth model against this backend's REST API (not addressed
  here — this engine phase has no authentication layer of its own beyond
  network placement; revisit before exposing the API UI publicly).
