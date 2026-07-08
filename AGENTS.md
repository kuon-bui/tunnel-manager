# Repository Guidelines

## Agent-Specific Instructions

AI agents working in this repository should read
`docs/ai-agent-architecture-conventions.md` before making backend changes. Use
that file as the detailed rulebook for package placement, dependency direction,
`fx` wiring, and the expected `route -> service -> repo` flow. When this file
and the architecture conventions file overlap, follow the more specific
instruction from `docs/ai-agent-architecture-conventions.md`.

## Project Structure & Module Organization

This repository is a Go backend for managing Cloudflare Tunnel domains. The
entrypoint is `cmd/server/main.go`. Runtime wiring lives in
`internal/application` and `internal/application/api` using `fx`. HTTP routes
and handlers live under `internal/application/api/route/<module>`. Business
logic lives in `internal/services/<module>`. Persistence and infra helpers live
under `internal/pkg`, especially `repo`, `config`, `cloudflare`, `process`, and
`sqlite`. Shared domain types live in `internal/model`. Database migrations are
stored in `migrations/`.

## Build, Test, and Development Commands

- `make run` starts the API with `go run ./cmd/server`.
- `make build` compiles all packages with `go build ./...`.
- `make test` runs `go test ./...`.
- `make migrate` applies Goose migrations using `DB_PATH` from `.env`.
- `make migrate-status` shows applied and pending migrations.

Typical local flow:

```bash
cp .env.example .env
make migrate
make run
```

## Coding Style & Naming Conventions

Follow standard Go formatting with tabs and `gofmt` style. Keep packages
lowercase and focused by module, for example `authservice`, `domainrepo`, and
`domainroute`. Keep handlers thin, place orchestration in services, and keep
Bun/SQLite queries inside repository packages. Prefer `fx.In` for constructors
with multiple dependencies. Add new request DTOs under `internal/pkg/request`.

## Testing Guidelines

There are currently no committed `*_test.go` files, but contributors should add
tests alongside new logic when practical. Use Go’s testing package and name
files `*_test.go` with `TestXxx` functions. Run `make test` before opening a
PR. For config, routing, or migration changes, also run `make build`.

## Commit & Pull Request Guidelines

Recent history uses short imperative commits with prefixes like `feat:`,
`fix:`, and `refactor:`. Keep that style, for example `feat: add audit login
history`. PRs should include a concise summary, note any config or migration
changes, list verification commands run, and include example API requests or
responses when endpoint behavior changes.

## Security & Configuration Tips

Do not commit `.env` or real credentials. Keep `.env.example` updated when
adding new environment variables. If you change schema or startup config, update
`README.md` and the relevant migration files in the same PR.
