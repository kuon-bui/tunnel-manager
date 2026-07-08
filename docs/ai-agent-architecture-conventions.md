# AI Agent Architecture Conventions

Audience: Codex and Claude Code working directly inside this repository.

Status: Active guidance for the current codebase, with notes about the target
architecture described in
`docs/superpowers/specs/2026-07-07-backend-module-architecture-design.md`.

## Purpose

This document tells an AI coding agent how to add or modify backend code in
this repository without breaking the intended architecture.

Use the current codebase as the source of truth for placement and dependency
direction. Use the refactor spec as the direction of travel, not as permission
to reorganize unrelated code during a normal feature change.

## Current Backend Shape

Today the backend follows this runtime flow:

```text
cmd/server/main.go
  -> internal/application.Module
  -> internal/application/api.Module
  -> route registration
  -> handler
  -> service
  -> repository / infrastructure package
  -> SQLite / Cloudflare / process supervisor
```

The important current packages are:

- `cmd/server/main.go`
  - Application entrypoint.
  - Keep it thin. It should only assemble `fx` modules and run the app.
- `internal/application/`
  - Top-level `fx` wiring for non-HTTP backend modules.
- `internal/application/api/`
  - HTTP server assembly and route module composition.
- `internal/application/api/route/auth`
  - Auth route registration and handlers.
- `internal/application/api/route/domain`
  - Domain route registration and handlers.
- `internal/services/auth`
  - Auth use cases such as login and admin bootstrap.
- `internal/services/domain`
  - Domain use cases such as create, list, get, update, stop, restart,
    reconcile, logs, metrics proxy.
- `internal/pkg/repo/auth`
  - Auth persistence abstraction and Bun-backed implementation.
- `internal/pkg/repo/domain`
  - Domain persistence abstraction and Bun-backed implementation.
- `internal/model`
  - Shared backend data shapes such as `Domain` and `Auth`.
- `internal/pkg/config`
  - Environment loading and validation.
- `internal/pkg/cloudflare`
  - Cloudflare API client.
- `internal/pkg/process`
  - `cloudflared` process supervision.
- `internal/pkg/sqlite`
  - Bun and SQLite DB bootstrap.
- `internal/pkg/request/...`
  - Request DTOs for handler binding and repo filtering.

## Source Of Truth Rules

When working on normal tasks, follow these priorities:

1. Existing code paths and module boundaries in the current repo.
2. This conventions file.
3. The target refactor spec.

If the current implementation and the target spec disagree, do not perform a
large refactor unless the task explicitly asks for it.

## Dependency Direction

Treat the backend as a layered system with downward-only dependencies:

```text
HTTP route/handler
  -> service
  -> repository or infrastructure helper
  -> external system
```

Apply these rules:

- Routes and handlers may depend on:
  - `gin`
  - request DTOs in `internal/pkg/request/...`
  - service interfaces in `internal/services/...`
  - middleware and config needed for HTTP setup
- Services may depend on:
  - `internal/model`
  - repository interfaces in `internal/pkg/repo/...`
  - infrastructure packages such as `cloudflare`, `process`, `portalloc`,
    `crypto`, `logbuf`, `config`, `constant`
- Repositories may depend on:
  - `internal/model`
  - Bun and SQL helpers
  - request/filter DTOs if already part of the existing repo contract
- Models must not depend on:
  - `gin`
  - route packages
  - service packages

Never invert these dependencies:

- Repository code must not import route or handler packages.
- Service code must not import route packages.
- Route code must not talk directly to SQLite, Bun, or Cloudflare unless the
  existing module already exposes that as a service operation.
- `main.go` must not manually wire individual concrete dependencies outside the
  `fx` module graph.

## Placement Rules

### 1. Route and handler code

Place HTTP-facing code in:

- `internal/application/api/route/<module>/route.go`
- `internal/application/api/route/<module>/handler.go`
- `internal/application/api/route/<module>/<action>.go`
- `internal/application/api/route/<module>/fx.go`

Use this pattern:

- `route.go`
  - build route groups
  - attach middleware
  - register endpoints
- `handler.go`
  - declare the handler struct
  - inject service dependencies through `fx.In`
- `<action>.go`
  - bind request input
  - call one service method
  - map errors to HTTP responses
  - write JSON or streamed response
- `fx.go`
  - expose the route through `common.ProvideAsRoute(...)`
  - provide the handler constructor

Handlers should stay thin. Do not move business rules, persistence queries, or
Cloudflare orchestration into handler methods.

### 2. Service code

Place business logic in:

- `internal/services/<module>/service.go`
- `internal/services/<module>/<action>.go`
- `internal/services/<module>/fx.go`

Use this pattern:

- `service.go`
  - declare the service interface
  - declare the concrete service struct
  - define constructor input via `fx.In`
- `<action>.go`
  - implement one use case or one closely related behavior
- `fx.go`
  - expose the constructor through an `fx.Module`

Services are the orchestration layer. This is where to:

- combine repository operations
- call Cloudflare APIs
- allocate ports
- encrypt or decrypt tokens
- coordinate process supervisor actions
- implement rollback or compensation logic

Do not place raw SQL/Bun query construction inside services.

### 3. Repository code

Place persistence code in:

- `internal/pkg/repo/<module>/repo.go`
- `internal/pkg/repo/<module>/<action>.go`

Use this pattern:

- `repo.go`
  - declare the repository interface
  - declare the concrete repo struct
  - hold the `*bun.DB`
- `<action>.go`
  - implement one query or one mutation

Repositories should:

- translate DB rows into `internal/model` values
- map not-found DB errors into `model.ErrNotFound` or shared repo helper errors
- keep SQL/Bun concerns local to the repository layer

Repositories should not:

- start or stop processes
- call Cloudflare APIs
- parse HTTP requests
- write HTTP responses

### 4. Model code

Place shared domain shapes in `internal/model`.

Guidance for this repo:

- Use `internal/model` as the canonical cross-layer business shape.
- Reuse existing types before adding duplicates.
- Prefer keeping new model types free from HTTP-only or DB-only concerns.

Note: the current codebase still has transitional model definitions such as
`internal/model/auth.go` embedding Bun metadata. Do not spread this pattern to
new code unless there is a strong compatibility reason.

### 5. Infrastructure and helpers

Place infrastructure adapters and shared utilities under `internal/pkg/...`.

Use existing package responsibilities:

- `config`
  - env loading and validation
- `cloudflare`
  - Cloudflare client
- `process`
  - local `cloudflared` supervision
- `sqlite`
  - DB bootstrap
- `middleware`
  - Gin middleware
- `request`
  - input DTOs and validation tags
- `constant`
  - stable enums and string constants

Do not create a new `pkg` package if an existing focused package already fits.

## FX Wiring Conventions

This repo uses `go.uber.org/fx`. Keep dependency wiring declarative.

Rules:

- Add constructors to the closest module `fx.go`.
- Group related providers under the existing module package.
- Prefer `fx.In` parameter structs for constructors with more than one
  dependency.
- Keep `cmd/server/main.go` thin and unchanged unless module composition
  itself changes.

Examples in the current codebase:

- `internal/application/fx.go` wires config, sqlite, cloudflare, process,
  repos, and services.
- `internal/application/api/route/auth/fx.go` and
  `internal/application/api/route/domain/fx.go` expose route registration.

If you add a new backend module, wire it in all required layers:

1. repository module or provider
2. service module
3. route module, if HTTP-exposed
4. parent `fx` aggregation module

## Configuration Conventions

Configuration belongs in `internal/pkg/config/config.go` and `.env.example`.

When adding a new environment variable:

1. Add it to `Config`.
2. Load it in `Load()`.
3. Validate it in `Load()` if required.
4. Add it to `.env.example`.
5. Update `README.md` if it changes operator-facing setup.

Do not call `os.Getenv` directly from feature code. Route, service, and repo
packages should receive config through constructors.

## Request DTO Conventions

Use `internal/pkg/request/<module>` for request payloads and query DTOs.

Rules:

- Handlers bind request bodies or query params into request DTOs.
- Services should receive already-shaped values, not raw `gin.Context`.
- Repositories may use request DTOs only when the current repo contract already
  does that, such as list filters and pagination.

Do not reuse model entities as request binding structs when the HTTP contract
and the persisted business model have different concerns.

## Error Handling Conventions

Follow the existing style:

- Handler layer
  - map errors to HTTP status codes and JSON error responses
- Service layer
  - wrap errors with operation context like `service: create tunnel: ...`
- Repository layer
  - map persistence not-found cases into a shared not-found error

Do not leak Bun-specific or SQL-specific errors straight to HTTP responses when
the service can translate them into a clearer domain error first.

## Current Playbooks

### Add a new authenticated endpoint

1. Add request DTOs under `internal/pkg/request/<module>` if needed.
2. Add a new handler method in
   `internal/application/api/route/<module>/<action>.go`.
3. Add or extend one service method in `internal/services/<module>`.
4. Add or extend repository methods only if persistence changes are required.
5. Register the route in `route.go`.
6. If the route needs JWT protection, place it under the existing authenticated
   route group.

### Add a new business use case

1. Start in `internal/services/<module>`.
2. Define or extend the service interface.
3. Implement orchestration in a dedicated `<action>.go`.
4. Add repository methods if the service needs new persistence behavior.
5. Expose the use case through HTTP only if the task explicitly needs a route.

### Add a new repository query

1. Extend the repository interface in `repo.go`.
2. Implement the query in a dedicated file like `get_by_<field>.go` or
   `list_<purpose>.go` if that naming matches the package style.
3. Keep Bun query construction and not-found mapping inside the repo package.
4. Return `internal/model` values, not Gin DTOs.

### Add a new config-backed capability

1. Extend `internal/pkg/config.Config`.
2. Validate config in `Load()`.
3. Thread the new config through the existing constructor input struct.
4. Do not read env variables directly inside handlers or services.

### Add a new backend module

For a new feature area such as `user`, `audit`, or `tunnelpolicy`, mirror the
existing slice-by-module layout:

- `internal/application/api/route/<module>/...`
- `internal/services/<module>/...`
- `internal/pkg/repo/<module>/...`
- `internal/pkg/request/<module>/...`
- `internal/model/<module>.go` if a new shared model is needed

Prefer following the `auth` and `domain` module shape instead of introducing a
different architecture for one feature.

## Do And Don't

Do:

- keep handlers thin
- keep services as the business orchestration layer
- keep repositories focused on persistence only
- extend existing modules before inventing new architectural patterns
- use `fx.Module`, `fx.Provide`, and `fx.In` consistently
- update `.env.example` and `README.md` when config changes affect operators

Do not:

- call repositories directly from `main.go`
- call Bun queries directly from handlers
- place Cloudflare calls inside repositories
- place JSON binding logic inside services
- add broad refactors while implementing a small feature
- reorganize package names toward the target refactor unless the task is
  explicitly architectural

## Relationship To The Target Refactor

The design spec in
`docs/superpowers/specs/2026-07-07-backend-module-architecture-design.md`
describes a cleaner future structure based on:

- `internal/application`
- `internal/interfaces/http`
- `internal/infrastructure/...`
- `internal/repositories`
- `internal/module`

Until that refactor is explicitly underway, AI agents should treat the current
repo structure as mandatory. Small improvements that move code toward the target
are good only when they are local, low-risk, and do not force unrelated file
moves.

## Task Completion Checklist For Agents

Before finishing a task, check:

- Did I put HTTP code only under route/handler packages?
- Did I keep business rules in services?
- Did I keep Bun/SQLite logic inside repositories?
- Did I wire new dependencies through `fx` instead of manual construction?
- Did I update config docs and `.env.example` if config changed?
- Did I preserve the current module pattern instead of inventing a new one?
- Did I avoid a refactor bigger than the requested change?

If any answer is no, revise the change before considering the task complete.
