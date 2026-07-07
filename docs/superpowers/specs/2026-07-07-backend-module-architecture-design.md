# Backend Module Architecture Refactor — Design Spec

Date: 2026-07-07
Status: Proposed

## Context

- `cmd/server/main.go` manually wires almost every dependency.
- `internal/store` mixes persistence concerns, schema definition, and the
  business-facing `Domain` shape.
- Environment configuration is read ad hoc via `os.Getenv`.
- Schema setup is done through repository code instead of versioned migrations.

The next round of work should keep the existing HTTP API behavior stable while
refactoring the backend into clearer internal modules with dependency injection,
explicit models, and operational tooling for local development and deployment.

## Goals

- Keep all existing external HTTP endpoints and their observable behavior
  working.
- Move business-facing models into `internal/model`.
- Move repository contracts and implementations under `internal/repositories`
  and stop using repository packages as the source of truth for models.
- Adopt `go.uber.org/fx` for dependency injection and application lifecycle.
- Adopt `viper` for environment loading while keeping the existing env variable
  names to avoid breaking deployment.
- Adopt `goose` for database migrations and remove schema creation from
  repository code.
- Add a `Makefile` that reads variables from `.env` for common tasks such as
  migration and application startup.

## Non-goals

- No change to the public REST API shape or route structure.
- No functional redesign of Cloudflare tunnel orchestration.
- No migration from SQLite to another database.
- No redesign of the frontend or observability stack.

## Architecture Decision

Three approaches were considered:

1. Package-by-layer refactor near the existing structure.
2. Clean-ish modular refactor with explicit boundaries and infrastructure
   adapters.
3. Full hexagonal split with command/query separation and deeper DTO layering.

This spec chooses approach 2.

Reasoning:

- It is strong enough to separate models from persistence and introduce DI
  cleanly.
- It keeps the refactor tractable for the current codebase and test suite.
- It avoids an unnecessarily large rewrite while still making later expansion
  easier.

## Target Package Layout

The backend will move toward the following structure:

```text
cmd/server/main.go
db/migrations/
internal/
  application/
    domainservice/
    ports/
  infrastructure/
    cloudflare/
    config/
    persistence/sqlite/
    process/
  interfaces/
    http/
  model/
  module/
  repositories/
  crypto/
  logbuf/
  portalloc/
```

### Module responsibilities

`internal/model`

- Owns business-facing types such as `Domain` and `Status`.
- Owns model-level errors such as `ErrNotFound`.
- Must not depend on Bun, SQLite, Gin, or Cloudflare SDK types.

`internal/repositories`

- Owns repository interfaces used by application services.
- Example: `DomainRepository`.
- Must depend on `internal/model`, not on Bun row structs.

`internal/application/domainservice`

- Owns the use cases already implemented today:
  `CreateDomain`, `ListDomains`, `GetDomain`, `UpdateOrigin`, `DeleteDomain`,
  `StopDomain`, `RestartDomain`, `Reconcile`, `HandleSupervisorEvent`.
- Depends on repository interfaces and application ports only.
- Must not depend directly on Bun, Gin, or concrete Cloudflare SDK clients.

`internal/application/ports`

- Owns non-repository ports for external dependencies.
- Expected ports include `CloudflareClient` and `ProcessSupervisor`.

`internal/infrastructure/persistence/sqlite`

- Owns Bun-specific database row structs, mapping logic, repository
  implementation, and migration runner integration.
- Database row structs stay private to infrastructure and are not used by the
  HTTP or application layers.

`internal/infrastructure/cloudflare`

- Implements the Cloudflare client port using `cloudflare-go/v6`.

`internal/infrastructure/process`

- Owns the subprocess supervisor now living in `internal/supervisor`.
- Emits events expressed in `internal/model.Status`, not repository package
  types.

`internal/infrastructure/config`

- Loads and validates configuration via `viper`.

`internal/interfaces/http`

- Owns Gin router and handlers.
- Talks to the application service interface and maps `model.Domain` to
  response DTOs.

`internal/module`

- Defines `fx.Module(...)` groupings for application assembly.

## Model And Repository Contracts

### Model

`internal/model.Domain` becomes the canonical domain entity for the backend.
It keeps the current business fields already visible in behavior and tests:

- `ID`
- `Hostname`
- `OriginURL`
- `CloudflareTunnelID`
- `DNSRecordID`
- `EncryptedTunnelToken`
- `Status`
- `MetricsPort`
- `PID`
- `RestartCount`
- `LastError`
- `CreatedAt`
- `UpdatedAt`

`internal/model.Status` keeps the same values currently in use:

- `pending`
- `active`
- `error`
- `stopped`

`internal/model.ErrNotFound` becomes the canonical not-found error used across
repository, application, and HTTP layers.

### Repository contract

`internal/repositories.DomainRepository` owns the persistence-facing contract:

- `Create(ctx, domain *model.Domain) error`
- `List(ctx) ([]model.Domain, error)`
- `Get(ctx, id string) (*model.Domain, error)`
- `GetByHostname(ctx, hostname string) (*model.Domain, error)`
- `ListByStatus(ctx, status model.Status) ([]model.Domain, error)`
- `Update(ctx, domain *model.Domain) error`
- `Delete(ctx, id string) error`

The repository is no longer responsible for schema creation. Migration is moved
out to the bootstrap/lifecycle layer through `goose`.

## Application Ports

The application layer will depend on interfaces instead of concrete
implementations:

- `CloudflareClient`
  - `CreateTunnel`
  - `PutIngressConfig`
  - `CreateDNSRecord`
  - `DeleteDNSRecord`
  - `DeleteTunnel`
- `ProcessSupervisor`
  - `Start`
  - `Stop`
  - `IsRunning`
  - event callback registration or injection-compatible event handling

`portalloc.Allocator`, `crypto`, and `logbuf` can remain concrete helpers for
now because they are already focused and local, but they are consumed by the
application module through constructors rather than global wiring in `main`.

## Persistence Design

The SQLite implementation under
`internal/infrastructure/persistence/sqlite` will:

- define a Bun row struct for the `domains` table
- map Bun rows to `model.Domain`
- map `model.Domain` back to Bun rows for insert/update
- translate `sql.ErrNoRows` to `model.ErrNotFound`

This preserves Bun as the SQL layer but isolates Bun-specific concerns from the
rest of the codebase.

## Configuration Design

Configuration moves from `os.Getenv` helpers to `viper`.

### Principles

- Keep the current environment variable names unchanged.
- Keep the current defaults unchanged where they already exist.
- Validate required values during startup.
- Keep parsing and validation centralized in one constructor.

### Expected config fields

- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_ZONE_ID`
- `ENCRYPTION_KEY`
- `DB_PATH`
- `LOG_DIR`
- `HTTP_ADDR`
- `METRICS_PORT_RANGE_START`
- `METRICS_PORT_RANGE_END`
- `CLOUDFLARED_BINARY`

### Behavior

`viper` will:

- call `AutomaticEnv()`
- set defaults for non-required fields
- load the existing env names directly
- decode `ENCRYPTION_KEY` from hex and require 32 bytes
- validate that the metrics range is well-formed

No config file format is introduced at this stage; `.env` is for local command
execution convenience through `Makefile`, not an application-specific runtime
config format.

## Dependency Injection And Lifecycle

The application bootstrap will move to `fx`.

### Module breakdown

- `module.Config`
- `module.Database`
- `module.Repositories`
- `module.Infrastructure`
- `module.Application`
- `module.HTTP`
- `module.Lifecycle`

### Construction flow

`cmd/server/main.go` becomes a thin `fx.New(...).Run()` entrypoint.

The DI graph will construct:

- validated config
- `*sql.DB`
- `*bun.DB`
- goose migration runner dependencies
- repository implementations
- Cloudflare adapter
- supervisor/process adapter
- allocator
- domain service
- Gin router
- `http.Server`

### Lifecycle hooks

The `fx` lifecycle will preserve the current operational behavior:

On start:

- create `LOG_DIR` if missing
- open SQLite database
- set `SetMaxOpenConns(1)` for SQLite write contention control
- run goose migrations up to latest
- `chmod` the DB file to `0600`
- call `Reconcile`
- start the HTTP server

On stop:

- gracefully shut down the HTTP server with the existing timeout behavior

This keeps `migrate -> reconcile -> serve` as the startup contract while
removing manual orchestration from `main.go`.

## Database Migrations With Goose

Schema management moves to `goose`.

### Migration location

- `db/migrations`

### Migration strategy

- Create an initial SQL migration that defines the `domains` table.
- Future schema changes must be added as new goose migrations, not hidden in
  repository code.
- The application startup path runs `goose up` before reconciliation.

### Operational reason

This makes schema evolution explicit, versioned, reviewable, and runnable in
local, CI, and production environments without embedding schema creation inside
repository constructors.

## Makefile And .env Workflow

The repo will gain local developer tooling centered around `.env`.

### Files

- `Makefile`
- `.env.example`

### Principles

- `Makefile` includes `.env` when present.
- Targets use env vars from `.env` without requiring manual export.
- The app still reads real environment variables at runtime through `viper`.

### Expected targets

- `make run`
- `make test`
- `make migrate-up`
- `make migrate-down`
- `make migrate-status`
- `make migrate-create name=<migration_name>`

If useful during implementation, additional safe targets may be added, such as
`make build` or `make fmt`.

### Migration CLI behavior

The migration targets use `goose` against the SQLite database identified by
`DB_PATH` from `.env`.

## Testing Strategy

The refactor should keep and adapt existing coverage rather than replacing it
blindly.

### Required preservation

- HTTP handler behavior tests remain valid at the endpoint contract level.
- Service tests remain valid at the orchestration behavior level.
- Supervisor tests remain valid at the subprocess lifecycle level.

### Required adaptations

- tests move from `internal/store` types to `internal/model`
- repository tests target the sqlite repository implementation under its new
  package
- config tests validate the `viper` loader behavior
- startup/bootstrap tests may be added where needed for migration and lifecycle
  wiring

### Refactor sequence

1. Introduce the new model and repository contracts.
2. Move repository implementation behind those contracts.
3. Update application and HTTP layers to consume `internal/model`.
4. Introduce `viper` config loading.
5. Introduce `goose` migrations.
6. Introduce `fx` modules and lifecycle.
7. Add `Makefile` and `.env.example`.

This sequence minimizes risk by preserving working behavior while replacing one
boundary at a time.

## Risks And Mitigations

### Risk: broad import churn

Moving `store.Domain` to `internal/model` will touch many files and tests.

Mitigation:

- do the move in a staged way
- keep the public method names stable where possible
- lean on existing tests while updating imports and types

### Risk: bootstrap regressions

Switching from manual startup wiring to `fx` can accidentally reorder boot
steps or hide errors.

Mitigation:

- keep lifecycle hooks explicit
- preserve the existing startup order
- verify migrations, reconcile, and HTTP startup with focused tests

### Risk: migration/runtime mismatch

Moving schema management from repository code to goose can fail if startup
paths or Makefile commands use different assumptions.

Mitigation:

- keep a single migration directory
- use the same DB path convention in app startup and Make targets
- validate against a temporary SQLite DB in tests where practical

## Success Criteria

The refactor is considered successful when:

- all current HTTP endpoints still behave as before
- models live under `internal/model`
- repository contracts live under `internal/repositories`
- no Bun model leaks into application or HTTP layers
- config loading uses `viper`
- startup uses `fx`
- schema migration uses `goose`
- common local workflows run through `Makefile` with `.env`

