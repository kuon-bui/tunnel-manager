# JWT Authentication — Design Spec

Date: 2026-07-08
Status: Proposed

## Context

The backend currently exposes `/api/domains/*` with no authentication. Anyone
who can reach the HTTP port can create, modify, or delete managed tunnels.
`internal/model/auth.go` and `internal/pkg/repo/auth/` already contain an
unfinished stub (`Auth{Username, Password}`, empty `AuthRepository`
interface) with no migration, no service, no route, and no JWT library wired
in yet.

This spec adds JWT-based authentication in front of the existing API,
following the same layered architecture used by the domain feature
(`model` → `pkg/repo/<feature>` → `services/<feature>` →
`application/api/route/<feature>`, wired through `go.uber.org/fx` at every
layer).

## Goals

- Require a valid JWT bearer token on every `/api/domains/*` route.
- Add a single public `POST /api/auth/login` endpoint that exchanges
  username/password for a token.
- Provision the one admin account automatically from environment
  configuration at startup — no public registration endpoint.
- Keep the change additive and consistent with existing patterns (fx
  modules, per-verb repo files, `model.ErrNotFound` conventions, viper-based
  config validation, goose migrations).

## Non-goals

- No RBAC or multi-user support. There is exactly one admin account.
- No refresh tokens, token revocation, or `/api/auth/logout` endpoint. The
  access token is stateless; expiry forces re-login.
- No self-service registration endpoint.
- No password-change API. Rotating the password means changing
  `ADMIN_PASSWORD` in `.env` and restarting the service.
- No login rate-limiting. The application has no rate-limiting
  infrastructure today; adding it is a separate concern.
- No automated tests for this feature (explicit user decision — the
  repository currently has no test suite and this change does not
  establish one).

## Decisions Confirmed With User

- **Admin provisioning**: seed from `.env` (`ADMIN_USERNAME`,
  `ADMIN_PASSWORD`) on startup. Rejected: public first-run register
  endpoint, manual DB/CLI seeding.
- **Token delivery**: `Authorization: Bearer <token>` header. Rejected:
  httpOnly cookie (would require CORS credential/SameSite changes for the
  cross-origin frontend at `CORS_ALLOWED_ORIGIN`).
- **Token strategy**: single access token (~7 days, configurable). Rejected:
  access+refresh pair (needs a revocation store and `/refresh` endpoint —
  unnecessary complexity for a single-operator internal tool).

## Data Model And Migration

`internal/model/auth.go` becomes:

```go
type Auth struct {
    bun.BaseModel `bun:"table:auths,alias:a"`

    Username  string    `bun:"username,pk" json:"username"`
    Password  string    `bun:"password,notnull" json:"-"`
    CreatedAt time.Time `bun:"created_at,notnull" json:"createdAt"`
    UpdatedAt time.Time `bun:"updated_at,notnull" json:"updatedAt"`
}
```

`Username` is the primary key. No surrogate UUID `id` is introduced (unlike
`model.Domain`) because nothing references an `Auth` row by foreign key and
exactly one row is expected to exist. `Password` stores a bcrypt hash and is
never serialized (`json:"-"`, matching `Domain.EncryptedTunnelToken`).

New migration `migrations/00002_create_auths.sql`:

```sql
-- +goose Up
CREATE TABLE auths (
    username TEXT PRIMARY KEY,
    password TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

-- +goose Down
DROP TABLE auths;
```

## Configuration

`internal/pkg/config/config.go` gains:

```go
AdminUsername string
AdminPassword string
JWTSecret     []byte
JWTTTL        time.Duration
```

Validation, following the existing `ENCRYPTION_KEY` pattern:

- `ADMIN_USERNAME`, `ADMIN_PASSWORD`: required; startup fails if either is
  empty.
- `JWT_SECRET`: hex-encoded, must decode to at least 32 bytes. Same
  generation instructions as `ENCRYPTION_KEY` (`openssl rand -hex 32`).
- `JWT_TTL`: optional. Parsed with `time.ParseDuration`; defaults to `168h`
  (7 days) when unset.

`.env.example` and the README configuration table gain these four
variables. The README HTTP API table gains the new login route and a note
that all `/api/domains/*` routes now require `Authorization: Bearer
<token>`.

## Package Layout

Mirrors the existing domain feature's file-per-verb structure exactly:

```
internal/pkg/request/auth/login.go      # LoginRequest{Username, Password string `binding:"required"`}

internal/pkg/repo/auth/repo.go          # fill AuthRepository interface (already stubbed)
internal/pkg/repo/auth/get.go           # GetByUsername — helpers.MapNotFound(err), same as domain Get
internal/pkg/repo/auth/create.go        # Create
internal/pkg/repo/auth/update.go        # Update — WherePK + RowsAffected check, same as domain Update

internal/pkg/jwt/jwt.go                 # GenerateToken(secret []byte, username string, ttl time.Duration) (token string, expiresAt time.Time, error)
                                         # ParseToken(secret []byte, token string) (username string, error)
                                         # wraps github.com/golang-jwt/jwt/v5 (imported as `jwtlib` to avoid
                                         # the package-name collision with this package's own name `jwt`)

internal/services/auth/service.go       # AuthService interface, struct, fx.In params, constructor
internal/services/auth/login.go         # Login(ctx, username, password) (token string, expiresAt time.Time, error)
internal/services/auth/bootstrap.go     # Bootstrap(ctx) error — seed/rotate admin, implements lifecycle.Bootstrapper
internal/services/auth/fx.go            # Module: NewAuthService, AsBootstrapper

internal/application/api/route/auth/route.go    # AuthRoute, group "/api/auth", no auth middleware
internal/application/api/route/auth/handler.go  # AuthHandler
internal/application/api/route/auth/login.go    # POST /login handler
internal/application/api/route/auth/fx.go       # Module: common.ProvideAsRoute(NewAuthRoute), fx.Provide(NewAuthHandler)

internal/pkg/middleware/auth.go         # JWTAuth(secret []byte) gin.HandlerFunc
```

Wiring changes to existing files:

- `internal/services/fx.go`: add `authservice.Module`.
- `internal/application/api/route/fx.go`: add `authroute.Module`.
- `internal/pkg/repo/fx.go`: no change — `authrepo.NewRepository` is already
  provided.
- `internal/application/api/route/domain/route.go`: `DomainRouteParams`
  gains `Cfg config.Config`. `NewDomainRoute` stores `params.Cfg.JWTSecret`
  on the `DomainRoute` struct (new `jwtSecret []byte` field). `Setup()`
  changes to `g := r.Group("/api/domains", middleware.JWTAuth(r.jwtSecret))`.

New dependency: `github.com/golang-jwt/jwt/v5` (`go get` required). Password
hashing uses `golang.org/x/crypto/bcrypt`, already present indirectly in
`go.sum`; a direct import is sufficient and `go mod tidy` will promote it to
a direct requirement.

Implementation note: per project convention, look up current
`golang-jwt/jwt/v5` API (claims struct, signing method constants) via the
`ctx7`/find-docs lookup before writing code against it, rather than relying
on training data.

## Lifecycle — Admin Bootstrap On Startup

`internal/pkg/lifecycle/lifecycle.go` currently accepts a single `Reconciler`
(non-fatal — failures are logged, not propagated) used only by the domain
service. This spec adds a second, distinctly-named seam using the same
group-tag pattern already used for `routes`:

```go
type Bootstrapper interface {
    Bootstrap(ctx context.Context) error
}
```

`LifecycleParams` gains `Bootstrappers []Bootstrapper `group:"bootstrappers"``.
`Register`'s `OnStart` runs bootstrappers first — before `mkdirAll`, `chmod`,
`Reconcile`, route setup, and server start — and **propagates any error**,
failing application startup.

This is intentionally not merged into the existing `Reconciler` mechanism:
a failed domain reconcile leaves the app running in a degraded-but-usable
state (today's behavior, preserved as-is), but a failed admin bootstrap
means no one can log in at all, which should block startup the same way an
invalid `ENCRYPTION_KEY` or Cloudflare credential already does.

`authservice.Bootstrap(ctx)` behavior:

1. `GetByUsername(cfg.AdminUsername)`.
2. Not found → hash `cfg.AdminPassword` with bcrypt, `Create` a row with
   `CreatedAt`/`UpdatedAt` set to `time.Now().UTC()` (same as
   `domainService.CreateDomain`).
3. Found → `bcrypt.CompareHashAndPassword` against `cfg.AdminPassword`; on
   mismatch, re-hash, set `UpdatedAt = time.Now().UTC()`, and `Update`. On
   match, no-op — `UpdatedAt` is left untouched.

This doubles as the password-rotation mechanism: change `ADMIN_PASSWORD` in
`.env` and restart. A username rename creates a new row rather than renaming
the old one in place; the orphaned row is left behind. This is an accepted
limitation for a single-admin tool.

## Request/Response Contracts

`POST /api/auth/login` (public, no middleware)

- Request body: `{"username": "...", "password": "..."}`, both
  `binding:"required"` (same style as `CreateDomainRequest`).
- `400`: JSON bind failure — same pattern as the existing `createDomain`
  handler.
- `401`: unknown username or wrong password — both cases return the same
  generic body, `{"error": "invalid credentials"}`, to avoid username
  enumeration.
- `200`: `{"token": "...", "expiresAt": "<RFC3339 timestamp>"}`.

`middleware.JWTAuth(secret)` (applied to the `/api/domains` group)

- Missing `Authorization` header, malformed `Bearer` prefix, or signature/
  expiry verification failure → `401`
  `c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})`, generic
  message in all cases.
- Valid token → `c.Next()`. Claims are not stored in the Gin context; there
  is currently no consumer for the authenticated username downstream. This
  can be added later if needed.

## Risks And Mitigations

### Risk: locking out the operator

If `JWT_SECRET`, `ADMIN_USERNAME`, or `ADMIN_PASSWORD` are misconfigured
after a working deployment, the bootstrap/login path fails.

Mitigation: bootstrap failures fail startup loudly (see Lifecycle section)
rather than silently leaving an unusable auth row; the fatal failure surfaces
in the same place operators already check for `ENCRYPTION_KEY`/Cloudflare
credential misconfiguration.

### Risk: breaking the existing frontend integration

Every `/api/domains/*` call from the existing frontend will start returning
`401` until it sends a valid bearer token.

Mitigation: this is an explicit, intended breaking change gated behind this
spec's approval; out of scope here, but the frontend must add a login step
and attach the `Authorization` header before this ships.

## Success Criteria

- `POST /api/auth/login` with the seeded admin credentials returns a valid
  JWT.
- Any `/api/domains/*` request without a valid bearer token returns `401`.
- Any `/api/domains/*` request with a valid bearer token behaves exactly as
  it does today.
- Changing `ADMIN_PASSWORD` in `.env` and restarting rotates the effective
  login password.
- Startup fails fast if `ADMIN_USERNAME`, `ADMIN_PASSWORD`, or `JWT_SECRET`
  are missing/invalid, consistent with existing config validation.
