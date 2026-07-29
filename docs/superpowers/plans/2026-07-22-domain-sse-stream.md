# Domain SSE Stream Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add continuously refreshed domain-list SSE, dual Bearer/cookie JWT authentication, cookie CSRF protection, and logout.

**Architecture:** Domain service owns a process-local capacity-one notification broadcaster and publishes after successful persistence writes. Domain route subscribes before loading an initial filtered snapshot, then reloads snapshots on notifications while sending heartbeat comments. Auth middleware strictly prefers Bearer when present, otherwise authenticates an `HttpOnly` cookie and validates Origin for unsafe cookie requests.

**Tech Stack:** Go 1.26.1, Gin 1.12.0, `net/http`, Go standard `testing`, Fx, existing JWT/auth/domain services.

## Global Constraints

- Preserve current `route -> service -> repository` dependency direction.
- Add no third-party dependency.
- SSE endpoint is `GET /api/domains/stream`; event name is `domains`; heartbeat interval is 15 seconds.
- Snapshot payload is `{"items":[...],"nextCursor":"..."}` and reuses `ListDomainRequest` filters/pagination.
- JWT cookie name is `tunnel_manager_token`, `HttpOnly=true`, `SameSite=Strict`, `Path=/api`.
- `AUTH_COOKIE_SECURE` defaults to `true`; local HTTP explicitly sets `false`.
- Bearer header has strict precedence; invalid Bearer never falls back to cookie.
- Unsafe cookie-authenticated requests require exact `Origin == CORS_ALLOWED_ORIGIN`.
- Broadcaster is process-local; keep `ponytail:` ceiling comment and upgrade path.
- Use TDD: run every targeted test red before production edits, then green.
- Streaming tests use `httptest.NewServer` plus a real HTTP client, not `httptest.ResponseRecorder`.

---

## File Map

### Create

- `internal/pkg/authcookie/cookie.go` — shared cookie name, set/clear helpers, and cookie configuration.
- `internal/pkg/authcookie/cookie_test.go` — exact cookie attribute tests.
- `internal/pkg/middleware/origin.go` — public-route Origin allowlist middleware.
- `internal/application/api/route/auth/handler_test.go` — login, password rotation, and logout HTTP behavior.
- `internal/services/domain/notify.go` — subscription lifecycle and notifying persistence wrappers.
- `internal/services/domain/notify_test.go` — broadcaster/coalescing/cancellation tests.
- `internal/application/api/route/domain/stream.go` — query binding, initial snapshot, SSE loop, heartbeat.
- `internal/application/api/route/domain/stream_test.go` — real-server streaming tests.

### Modify

- `internal/pkg/config/config.go` — add `AuthCookieSecure bool`.
- `.env.example` — document local `AUTH_COOKIE_SECURE=false`.
- `internal/pkg/middleware/auth.go` — Bearer/cookie selection and unsafe-request Origin check.
- `internal/pkg/middleware/auth_test.go` — dual-auth and CSRF tests.
- `internal/pkg/middleware/cors.go` — credentialed CORS for exact configured origin.
- `internal/application/api/route/auth/handler.go` — inject cookie configuration.
- `internal/application/api/route/auth/login.go` — issue cookie.
- `internal/application/api/route/auth/change_password.go` — replace cookie after token rotation.
- `internal/application/api/route/auth/route.go` — register Origin-protected logout route.
- `internal/services/domain/service.go` — add subscribe method, subscriber fields, and initialize broadcaster.
- `internal/services/domain/create.go` — use notifying create/update wrappers.
- `internal/services/domain/update.go` — use notifying update wrapper.
- `internal/services/domain/delete.go` — use notifying delete wrapper.
- `internal/services/domain/stop.go` — use notifying update wrapper.
- `internal/services/domain/restart.go` — use notifying update wrapper.
- `internal/application/api/route/domain/route.go` — register stream before `/:id` routes.
- `README.md` — document cookie config, auth behavior, logout, SSE contract.

---

### Task 1: Cookie Configuration and Helpers

**Files:**
- Create: `internal/pkg/authcookie/cookie.go`
- Create: `internal/pkg/authcookie/cookie_test.go`
- Modify: `internal/pkg/config/config.go`
- Modify: `.env.example`

**Interfaces:**
- Consumes: `config.Config.AuthCookieSecure bool`, JWT expiry `time.Time`.
- Produces: `authcookie.Name`, `authcookie.Set(http.ResponseWriter, string, time.Time, bool)`, `authcookie.Clear(http.ResponseWriter, bool)`.

- [ ] **Step 1: Write failing helper tests**

Create `internal/pkg/authcookie/cookie_test.go`:

```go
package authcookie

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSetWritesSecureHTTPOnlyStrictCookie(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	res := httptest.NewRecorder()

	Set(res, "jwt", expiresAt, true)

	cookies := res.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != Name || cookie.Value != "jwt" || cookie.Path != "/api" {
		t.Fatalf("cookie = %#v", cookie)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("security attributes = %#v", cookie)
	}
	if !cookie.Expires.Equal(expiresAt) || cookie.MaxAge <= 0 {
		t.Fatalf("expiry = %v/%d", cookie.Expires, cookie.MaxAge)
	}
}

func TestClearExpiresCookie(t *testing.T) {
	res := httptest.NewRecorder()

	Clear(res, false)

	cookie := res.Result().Cookies()[0]
	if cookie.Name != Name || cookie.Value != "" || cookie.Path != "/api" {
		t.Fatalf("cookie = %#v", cookie)
	}
	if cookie.MaxAge >= 0 || cookie.Expires.After(time.Now()) || cookie.Secure {
		t.Fatalf("clear attributes = %#v", cookie)
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("security attributes = %#v", cookie)
	}
}
```

- [ ] **Step 2: Run helper tests and verify red**

Run: `go test ./internal/pkg/authcookie -run 'Test(Set|Clear)' -count=1`

Expected: FAIL because package/files or `Set`, `Clear`, and `Name` do not exist.

- [ ] **Step 3: Add config field and minimal cookie helper**

Add to `config.Config`:

```go
AuthCookieSecure bool
```

Before the `cfg := Config{...}` literal in `Load()`, set secure default:

```go
v.SetDefault("AUTH_COOKIE_SECURE", true)
```

Add to the literal:

```go
AuthCookieSecure: v.GetBool("AUTH_COOKIE_SECURE"),
```

Create `internal/pkg/authcookie/cookie.go`:

```go
package authcookie

import (
	"net/http"
	"time"
)

const Name = "tunnel_manager_token"

func Set(w http.ResponseWriter, token string, expiresAt time.Time, secure bool) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(w, &http.Cookie{
		Name:     Name,
		Value:    token,
		Path:     "/api",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func Clear(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     Name,
		Path:     "/api",
		Expires:  time.Unix(1, 0).UTC(),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}
```

Add to `.env.example`:

```env
AUTH_COOKIE_SECURE=false
```

- [ ] **Step 4: Run helper and config tests**

Run: `go test ./internal/pkg/authcookie ./internal/pkg/config -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/authcookie internal/pkg/config/config.go .env.example
git commit -m "feat(auth): add secure JWT cookie helpers"
```

---

### Task 2: Dual JWT Authentication and CSRF Enforcement

**Files:**
- Modify: `internal/pkg/middleware/auth.go`
- Modify: `internal/pkg/middleware/auth_test.go`
- Modify: `internal/application/api/route/auth/route.go`
- Modify: `internal/application/api/route/domain/route.go`

**Interfaces:**
- Consumes: `authcookie.Name`, `config.Config.CORSAllowedOrigin`, `Authenticator.Authenticate`.
- Produces: `JWTAuth(authenticator Authenticator, cfg config.Config) gin.HandlerFunc`, context key `AuthenticationSourceKey`, values `AuthenticationSourceBearer` and `AuthenticationSourceCookie`.

- [ ] **Step 1: Replace middleware tests with dual-auth cases**

Update `internal/pkg/middleware/auth_test.go` so the request helper accepts method, header, cookie, Origin, and config. Preserve existing missing/malformed/authenticator-error/username cases and add:

```go
func TestJWTAuthUsesCookieWhenBearerAbsent(t *testing.T) {
	authenticator := &fakeAuthenticator{username: "admin"}
	status, username := authenticatedRequest(t, http.MethodGet, "", "cookie-token", "", authenticator)
	if status != http.StatusOK || username != "admin" || authenticator.token != "cookie-token" {
		t.Fatalf("status/username/token = %d/%q/%q", status, username, authenticator.token)
	}
}

func TestJWTAuthDoesNotFallbackFromInvalidBearerToCookie(t *testing.T) {
	status, _ := authenticatedRequest(t, http.MethodGet, "Token bad", "cookie-token", "", &fakeAuthenticator{username: "admin"})
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d", status)
	}
}

func TestJWTAuthAllowsUnsafeBearerWithoutOrigin(t *testing.T) {
	status, _ := authenticatedRequest(t, http.MethodPost, "Bearer header-token", "", "", &fakeAuthenticator{username: "admin"})
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
}

func TestJWTAuthChecksOriginForUnsafeCookieRequest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		origin string
		want   int
	}{
		{name: "matching", origin: "https://app.example.com", want: http.StatusOK},
		{name: "missing", want: http.StatusForbidden},
		{name: "mismatch", origin: "https://evil.example.com", want: http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, _ := authenticatedRequest(t, http.MethodPost, "", "cookie-token", tc.origin, &fakeAuthenticator{username: "admin"})
			if status != tc.want {
				t.Fatalf("status = %d", status)
			}
		})
	}
}
```

Make helper configure middleware with:

```go
JWTAuth(authenticator, config.Config{CORSAllowedOrigin: "https://app.example.com"})
```

Set cookie using:

```go
req.AddCookie(&http.Cookie{Name: authcookie.Name, Value: cookieToken})
```

- [ ] **Step 2: Run middleware tests and verify red**

Run: `go test ./internal/pkg/middleware -run TestJWTAuth -count=1`

Expected: FAIL because `JWTAuth` lacks config/cookie/CSRF behavior.

- [ ] **Step 3: Implement strict token selection and Origin check**

Change signature and implementation in `auth.go`:

```go
const (
	AuthenticatedUsernameKey       = "authenticatedUsername"
	AuthenticationSourceKey        = "authenticationSource"
	AuthenticationSourceBearer     = "bearer"
	AuthenticationSourceCookie     = "cookie"
)

func JWTAuth(authenticator Authenticator, cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, source, ok := authenticationToken(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		username, err := authenticator.Authenticate(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		if source == AuthenticationSourceCookie && !safeMethod(c.Request.Method) && (cfg.CORSAllowedOrigin == "" || c.GetHeader("Origin") != cfg.CORSAllowedOrigin) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Set(AuthenticatedUsernameKey, username)
		c.Set(AuthenticationSourceKey, source)
		c.Next()
	}
}

func authenticationToken(c *gin.Context) (string, string, bool) {
	if header := c.GetHeader("Authorization"); header != "" {
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" || strings.ContainsAny(token, " \t\r\n") {
			return "", "", false
		}
		return token, AuthenticationSourceBearer, true
	}
	token, err := c.Cookie(authcookie.Name)
	if err != nil || token == "" {
		return "", "", false
	}
	return token, AuthenticationSourceCookie, true
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}
```

Import `tunnelmanager/internal/pkg/authcookie` and `tunnelmanager/internal/pkg/config`.

Update both call sites:

```go
middleware.JWTAuth(r.authService, r.cfg)
```

Add `cfg config.Config` to `AuthRoute` and `DomainRoute`; inject from their existing `fx.In` params.

- [ ] **Step 4: Run middleware and route package tests**

Run: `go test ./internal/pkg/middleware ./internal/application/api/route/auth ./internal/application/api/route/domain -count=1`

Expected: PASS and compile succeeds with new signature.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/middleware/auth.go internal/pkg/middleware/auth_test.go internal/application/api/route/auth/route.go internal/application/api/route/domain/route.go
git commit -m "feat(auth): accept Bearer and cookie JWTs"
```

---

### Task 3: Login, Password Rotation, Logout, and Public Origin Guard

**Files:**
- Create: `internal/pkg/middleware/origin.go`
- Create: `internal/application/api/route/auth/handler_test.go`
- Create: `internal/application/api/route/auth/logout.go`
- Modify: `internal/application/api/route/auth/handler.go`
- Modify: `internal/application/api/route/auth/login.go`
- Modify: `internal/application/api/route/auth/change_password.go`
- Modify: `internal/application/api/route/auth/route.go`

**Interfaces:**
- Consumes: `authcookie.Set`, `authcookie.Clear`, `config.Config.AuthCookieSecure`, exact `CORSAllowedOrigin`.
- Produces: `middleware.RequireAllowedOrigin(cfg config.Config) gin.HandlerFunc`, `POST /api/auth/logout`.

- [ ] **Step 1: Write failing route tests**

Create `internal/application/api/route/auth/handler_test.go` with a local fake implementing `authservice.AuthService` and these tests:

```go
package authroute

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tunnelmanager/internal/pkg/authcookie"
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/middleware"

	"github.com/gin-gonic/gin"
)

type fakeAuthService struct {
	token     string
	expiresAt time.Time
	err       error
}

func (f *fakeAuthService) Login(context.Context, string, string) (string, time.Time, error) {
	return f.token, f.expiresAt, f.err
}

func (f *fakeAuthService) Authenticate(context.Context, string) (string, error) {
	return "admin", f.err
}

func (f *fakeAuthService) ChangePassword(context.Context, string, string, string) (string, time.Time, error) {
	return f.token, f.expiresAt, f.err
}

func (f *fakeAuthService) Bootstrap(context.Context) error { return f.err }

func TestLoginSetsCookieAndKeepsTokenResponse(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour).UTC()
	h := NewAuthHandler(AuthHandlerParams{
		AuthService: &fakeAuthService{token: "jwt", expiresAt: expiresAt},
		Cfg:         config.Config{AuthCookieSecure: true},
	})
	r := gin.New()
	r.POST("/login", h.login)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"password"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"token":"jwt"`) {
		t.Fatalf("response = %d %s", res.Code, res.Body.String())
	}
	cookie := res.Result().Cookies()[0]
	if cookie.Name != authcookie.Name || cookie.Value != "jwt" || !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("cookie = %#v", cookie)
	}
}

func TestChangePasswordReplacesCookie(t *testing.T) {
	expiresAt := time.Now().Add(time.Hour).UTC()
	h := NewAuthHandler(AuthHandlerParams{
		AuthService: &fakeAuthService{token: "replacement", expiresAt: expiresAt},
		Cfg:         config.Config{},
	})
	r := gin.New()
	r.PUT("/password", func(c *gin.Context) {
		c.Set(middleware.AuthenticatedUsernameKey, "admin")
		c.Next()
	}, h.changePassword)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/password", strings.NewReader(`{"currentPassword":"old-password-123","newPassword":"new-password-456"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(res, req)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"token":"replacement"`) {
		t.Fatalf("response = %d %s", res.Code, res.Body.String())
	}
	cookie := res.Result().Cookies()[0]
	if cookie.Name != authcookie.Name || cookie.Value != "replacement" || !cookie.HttpOnly {
		t.Fatalf("cookie = %#v", cookie)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	h := NewAuthHandler(AuthHandlerParams{
		AuthService: &fakeAuthService{},
		Cfg:         config.Config{AuthCookieSecure: true},
	})
	r := gin.New()
	r.POST("/logout", h.logout)
	res := httptest.NewRecorder()
	r.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/logout", nil))
	if res.Code != http.StatusNoContent {
		t.Fatalf("status = %d", res.Code)
	}
	cookie := res.Result().Cookies()[0]
	if cookie.Name != authcookie.Name || cookie.MaxAge >= 0 || !cookie.Secure {
		t.Fatalf("cookie = %#v", cookie)
	}
}

func TestRequireAllowedOrigin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		origin string
		want   int
	}{
		{name: "matching", origin: "https://app.example.com", want: http.StatusNoContent},
		{name: "mismatched", origin: "https://evil.example.com", want: http.StatusForbidden},
		{name: "non-browser", want: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := gin.New()
			r.POST("/", middleware.RequireAllowedOrigin(config.Config{CORSAllowedOrigin: "https://app.example.com"}), func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})
			res := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			r.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status = %d", res.Code)
			}
		})
	}
}
```

- [ ] **Step 2: Run auth route tests and verify red**

Run: `go test ./internal/application/api/route/auth -run 'Test(Login|ChangePassword|Logout|RequireAllowedOrigin)' -count=1`

Expected: FAIL because config injection, cookie writes, logout, and Origin guard do not exist.

- [ ] **Step 3: Implement handler config and cookie writes**

Extend handler:

```go
type AuthHandler struct {
	authService authservice.AuthService
	cfg         config.Config
}

type AuthHandlerParams struct {
	fx.In
	AuthService authservice.AuthService
	Cfg         config.Config
}
```

After successful login and password change, before JSON response:

```go
authcookie.Set(c.Writer, token, expiresAt, h.cfg.AuthCookieSecure)
```

Implement logout:

```go
func (h *AuthHandler) logout(c *gin.Context) {
	authcookie.Clear(c.Writer, h.cfg.AuthCookieSecure)
	c.Status(http.StatusNoContent)
}
```

- [ ] **Step 4: Implement public Origin guard and route**

Create `origin.go`:

```go
func RequireAllowedOrigin(cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && origin != cfg.CORSAllowedOrigin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.Next()
	}
}
```

Store `cfg config.Config` on `AuthRoute`, then register:

```go
g.POST("/login", middleware.RequireAllowedOrigin(r.cfg), r.authHandler.login)
g.POST("/logout", middleware.RequireAllowedOrigin(r.cfg), r.authHandler.logout)
```

Keep password route under `JWTAuth`.

- [ ] **Step 5: Run auth and middleware tests**

Run: `go test ./internal/application/api/route/auth ./internal/pkg/middleware -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/application/api/route/auth internal/pkg/middleware/origin.go
git commit -m "feat(auth): issue and clear JWT cookie"
```

---

### Task 4: Credentialed CORS

**Files:**
- Modify: `internal/pkg/middleware/cors.go`
- Create: `internal/pkg/middleware/cors_test.go`

**Interfaces:**
- Consumes: exact `allowedOrigin string`.
- Produces: configured-origin CORS response with `Access-Control-Allow-Credentials: true`.

- [ ] **Step 1: Write failing CORS tests**

Create `cors_test.go`:

```go
func TestCorsMiddlewareAllowsConfiguredOriginWithCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CorsMiddleware("https://app.example.com"))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	r.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Fatalf("origin = %q", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("credentials = %q", got)
	}
}

func TestCorsMiddlewareDoesNotEchoUntrustedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CorsMiddleware("https://app.example.com"))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	r.ServeHTTP(res, req)
	if got := res.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("origin = %q", got)
	}
	if got := res.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("credentials = %q", got)
	}
}
```

- [ ] **Step 2: Run CORS tests and verify red**

Run: `go test ./internal/pkg/middleware -run TestCorsMiddleware -count=1`

Expected: FAIL because credentials header is absent and current middleware emits configured origin without checking request Origin.

- [ ] **Step 3: Implement exact-origin credentialed CORS**

Use:

```go
origin := c.GetHeader("Origin")
if allowedOrigin != "" && origin == allowedOrigin {
	c.Header("Access-Control-Allow-Origin", allowedOrigin)
	c.Header("Access-Control-Allow-Credentials", "true")
	c.Header("Vary", "Origin")
	c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
}
```

For `OPTIONS`, return `204`; browsers reject untrusted preflight because allow headers are absent.

- [ ] **Step 4: Run middleware tests**

Run: `go test ./internal/pkg/middleware -count=1`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/middleware/cors.go internal/pkg/middleware/cors_test.go
git commit -m "feat(auth): allow credentialed CORS"
```

---

### Task 5: Domain Change Broadcaster and Persistence Notifications

**Files:**
- Create: `internal/services/domain/notify.go`
- Create: `internal/services/domain/notify_test.go`
- Modify: `internal/services/domain/service.go`
- Modify: `internal/services/domain/create.go`
- Modify: `internal/services/domain/update.go`
- Modify: `internal/services/domain/delete.go`
- Modify: `internal/services/domain/stop.go`
- Modify: `internal/services/domain/restart.go`

**Interfaces:**
- Consumes: existing `DomainRepository` write methods.
- Produces: `DomainService.Subscribe() (<-chan struct{}, func())`; private `create`, `update`, `updateBulk`, and `delete` wrappers that notify after successful writes.

- [ ] **Step 1: Write failing broadcaster tests**

Create tests in package `domainservice`:

```go
func TestPublishNotifiesAndCoalesces(t *testing.T) {
	s := &domainService{subscribers: make(map[chan struct{}]struct{})}
	updates, cancel := s.Subscribe()
	defer cancel()

	s.publish()
	s.publish()

	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("missing notification")
	}
	select {
	case <-updates:
		t.Fatal("notifications did not coalesce")
	default:
	}
}

func TestPublishDoesNotBlockOnSlowSubscriber(t *testing.T) {
	s := &domainService{subscribers: make(map[chan struct{}]struct{})}
	_, cancelSlow := s.Subscribe()
	defer cancelSlow()
	fast, cancelFast := s.Subscribe()
	defer cancelFast()
	s.publish()
	select {
	case <-fast:
	case <-time.After(time.Second):
		t.Fatal("fast subscriber missed notification")
	}

	done := make(chan struct{})
	go func() {
		s.publish()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked")
	}
	select {
	case <-fast:
	case <-time.After(time.Second):
		t.Fatal("fast subscriber missed second notification")
	}
}

func TestCancelSubscriptionIsIdempotent(t *testing.T) {
	s := &domainService{subscribers: make(map[chan struct{}]struct{})}
	updates, cancel := s.Subscribe()
	cancel()
	cancel()
	s.publish()
	select {
	case <-updates:
		t.Fatal("cancelled subscriber received notification")
	default:
	}
}
```

Use an embedded repository interface so only the exercised write needs implementation, then test one wrapper:

```go
type notifyRepo struct {
	domainrepo.DomainRepository
	updateErr error
}

func (r *notifyRepo) Update(context.Context, *model.Domain) error {
	return r.updateErr
}

func TestUpdatePublishesOnlyAfterSuccessfulWrite(t *testing.T) {
	success := &domainService{
		repo:        &notifyRepo{},
		subscribers: make(map[chan struct{}]struct{}),
	}
	updates, cancel := success.Subscribe()
	defer cancel()
	if err := success.update(context.Background(), &model.Domain{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
	default:
		t.Fatal("successful update did not publish")
	}

	wantErr := errors.New("write failed")
	failure := &domainService{
		repo:        &notifyRepo{updateErr: wantErr},
		subscribers: make(map[chan struct{}]struct{}),
	}
	failedUpdates, cancelFailed := failure.Subscribe()
	defer cancelFailed()
	if err := failure.update(context.Background(), &model.Domain{}); !errors.Is(err, wantErr) {
		t.Fatalf("err = %v", err)
	}
	select {
	case <-failedUpdates:
		t.Fatal("failed update published")
	default:
	}
}
```

- [ ] **Step 2: Run broadcaster tests and verify red**

Run: `go test ./internal/services/domain -run 'Test(Publish|Cancel|UpdatePublishes)' -count=1`

Expected: FAIL because broadcaster and wrappers do not exist.

- [ ] **Step 3: Add subscription state and implementation**

In `domainService` add a dedicated mutex and map:

```go
subscriberMu sync.Mutex
subscribers  map[chan struct{}]struct{}
```

Initialize in `NewDomainService`:

```go
subscribers: make(map[chan struct{}]struct{}),
```

Add interface method:

```go
Subscribe() (<-chan struct{}, func())
```

Create `notify.go` with capacity-one non-blocking publish and `sync.Once` cancellation:

```go
func (s *domainService) Subscribe() (<-chan struct{}, func()) {
	updates := make(chan struct{}, 1)
	s.subscriberMu.Lock()
	s.subscribers[updates] = struct{}{}
	s.subscriberMu.Unlock()
	var once sync.Once
	return updates, func() {
		once.Do(func() {
			s.subscriberMu.Lock()
			delete(s.subscribers, updates)
			s.subscriberMu.Unlock()
		})
	}
}

func (s *domainService) publish() {
	// ponytail: Process-local fan-out supports one backend process; use shared pub/sub before adding replicas.
	s.subscriberMu.Lock()
	defer s.subscriberMu.Unlock()
	for updates := range s.subscribers {
		select {
		case updates <- struct{}{}:
		default:
		}
	}
}
```

- [ ] **Step 4: Add notifying persistence wrappers**

In `notify.go`:

```go
func (s *domainService) create(ctx context.Context, domain *model.Domain) error {
	if err := s.repo.Create(ctx, domain); err != nil { return err }
	s.publish()
	return nil
}

func (s *domainService) update(ctx context.Context, domain *model.Domain) error {
	if err := s.repo.Update(ctx, domain); err != nil { return err }
	s.publish()
	return nil
}

func (s *domainService) updateBulk(ctx context.Context, domains []*model.Domain) error {
	if len(domains) == 0 { return nil }
	if err := s.repo.UpdateBulk(ctx, domains); err != nil { return err }
	s.publish()
	return nil
}

func (s *domainService) delete(ctx context.Context, id string) error {
	if err := s.repo.Delete(ctx, id); err != nil { return err }
	s.publish()
	return nil
}
```

Format these as normal multi-line Go.

Replace domain-service persistence calls:

- `s.repo.Create` with `s.create`.
- All frontend-visible `s.repo.Update` writes with `s.update`.
- `s.repo.UpdateBulk` with `s.updateBulk`.
- `s.repo.Delete` with `s.delete`.

Do not wrap reads.

- [ ] **Step 5: Run domain service tests**

Run: `go test ./internal/services/domain -count=1`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/services/domain
git commit -m "feat(domain): broadcast persisted changes"
```

---

### Task 6: Domain SSE Endpoint

**Files:**
- Create: `internal/application/api/route/domain/stream.go`
- Create: `internal/application/api/route/domain/stream_test.go`
- Modify: `internal/application/api/route/domain/route.go`

**Interfaces:**
- Consumes: `DomainService.Subscribe`, `DomainService.ListDomains`, `domainrequest.ListDomainRequest`.
- Produces: `GET /api/domains/stream`, event `domains`, 15-second `: heartbeat` comments.

- [ ] **Step 1: Write failing real-server test for initial event**

Create `stream_test.go`. Embed `DomainService` so fake only overrides methods exercised by stream handler, then add SSE parser and state helpers:

```go
package domainroute

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"tunnelmanager/internal/model"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
	domainservice "tunnelmanager/internal/services/domain"

	"github.com/gin-gonic/gin"
)

type fakeDomainService struct {
	domainservice.DomainService
	mu          sync.Mutex
	domains     []*model.Domain
	nextCursor  string
	listErr     error
	lastRequest domainrequest.ListDomainRequest
	updates     chan struct{}
	cancelled   chan struct{}
	cancelOnce  sync.Once
}

func (f *fakeDomainService) ListDomains(_ context.Context, req domainrequest.ListDomainRequest) ([]*model.Domain, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastRequest = req
	return f.domains, f.nextCursor, f.listErr
}

func (f *fakeDomainService) Subscribe() (<-chan struct{}, func()) {
	return f.updates, func() { f.cancelOnce.Do(func() { close(f.cancelled) }) }
}

func (f *fakeDomainService) setList(domains []*model.Domain, err error) {
	f.mu.Lock()
	f.domains = domains
	f.listErr = err
	f.mu.Unlock()
}

func (f *fakeDomainService) request() domainrequest.ListDomainRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastRequest
}

type sseEvent struct {
	name    string
	data    string
	comment string
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) sseEvent {
	t.Helper()
	var event sseEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			return event
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			event.name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			event.data = strings.TrimPrefix(line, "data: ")
		case strings.HasPrefix(line, ": "):
			event.comment = strings.TrimPrefix(line, ": ")
		}
	}
}

func openDomainStream(t *testing.T, service *fakeDomainService, rawQuery string) (*http.Response, *bufio.Reader, context.CancelFunc) {
	t.Helper()
	h := &DomainHandler{domainService: service}
	r := gin.New()
	r.GET("/stream", h.streamDomains)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stream"+rawQuery, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp, bufio.NewReader(resp.Body), cancel
}

func TestStreamDomainsSendsInitialFilteredSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeDomainService{
		domains: []*model.Domain{{ID: "domain-1", Hostname: "api.example.com"}},
		updates: make(chan struct{}, 1), cancelled: make(chan struct{}),
	}
	resp, reader, cancel := openDomainStream(t, service, "?hostname=api&pageSize=20")
	defer cancel()
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content type = %q", got)
	}
	if resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("stream headers = %#v", resp.Header)
	}
	event := readSSEEvent(t, reader)
	if event.name != "domains" || !strings.Contains(event.data, `"id":"domain-1"`) {
		t.Fatalf("event = %#v", event)
	}
	req := service.request()
	if req.Hostname != "api" || req.PageSize != 20 {
		t.Fatalf("request = %#v", req)
	}
}
```

- [ ] **Step 2: Run initial stream test and verify red**

Run: `go test ./internal/application/api/route/domain -run TestStreamDomainsSendsInitialFilteredSnapshot -count=1`

Expected: FAIL because `streamDomains` does not exist.

- [ ] **Step 3: Implement minimal stream handler and route**

Create `stream.go`:

```go
const domainStreamHeartbeatInterval = 15 * time.Second

type domainListSnapshot struct {
	Items      []*model.Domain `json:"items"`
	NextCursor string          `json:"nextCursor"`
}

func (h *DomainHandler) streamDomains(c *gin.Context) {
	var req domainrequest.ListDomainRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	updates, cancel := h.domainService.Subscribe()
	defer cancel()
	items, nextCursor, err := h.domainService.ListDomains(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	c.SSEvent("domains", domainListSnapshot{Items: items, NextCursor: nextCursor})
	c.Writer.Flush()

	heartbeat := time.NewTicker(domainStreamHeartbeatInterval)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-updates:
			items, nextCursor, err = h.domainService.ListDomains(c.Request.Context(), req)
			if err != nil {
				c.SSEvent("error", gin.H{"message": "stream unavailable"})
				c.Writer.Flush()
				return
			}
			c.SSEvent("domains", domainListSnapshot{Items: items, NextCursor: nextCursor})
			c.Writer.Flush()
		case <-heartbeat.C:
			_, _ = c.Writer.WriteString(": heartbeat\n\n")
			c.Writer.Flush()
		}
	}
}
```

Register before `g.GET("/:id", ...)`:

```go
g.GET("/stream", r.domainHandler.streamDomains)
```

- [ ] **Step 4: Run initial stream test and verify green**

Run: `go test ./internal/application/api/route/domain -run TestStreamDomainsSendsInitialFilteredSnapshot -count=1`

Expected: PASS.

- [ ] **Step 5: Add notification, error, heartbeat, and disconnect tests**

Add:

```go
func TestStreamDomainsSendsSnapshotAfterNotification(t *testing.T) {
	service := &fakeDomainService{
		domains: []*model.Domain{{ID: "domain-1"}},
		updates: make(chan struct{}, 1), cancelled: make(chan struct{}),
	}
	_, reader, cancel := openDomainStream(t, service, "")
	defer cancel()
	_ = readSSEEvent(t, reader)
	service.setList([]*model.Domain{{ID: "domain-2"}}, nil)
	service.updates <- struct{}{}
	event := readSSEEvent(t, reader)
	if event.name != "domains" || !strings.Contains(event.data, `"id":"domain-2"`) {
		t.Fatalf("event = %#v", event)
	}
}

func TestStreamDomainsSendsGenericErrorAndCloses(t *testing.T) {
	service := &fakeDomainService{
		domains: []*model.Domain{{ID: "domain-1"}},
		updates: make(chan struct{}, 1), cancelled: make(chan struct{}),
	}
	resp, reader, cancel := openDomainStream(t, service, "")
	defer cancel()
	_ = readSSEEvent(t, reader)
	service.setList(nil, errors.New("database unavailable"))
	service.updates <- struct{}{}
	event := readSSEEvent(t, reader)
	if event.name != "error" || event.data != `{"message":"stream unavailable"}` {
		t.Fatalf("event = %#v", event)
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("stream remained open: %v", err)
	}
	_ = resp
}

func TestStreamDomainsSendsHeartbeat(t *testing.T) {
	oldInterval := domainStreamHeartbeatInterval
	domainStreamHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { domainStreamHeartbeatInterval = oldInterval })
	service := &fakeDomainService{updates: make(chan struct{}, 1), cancelled: make(chan struct{})}
	_, reader, cancel := openDomainStream(t, service, "")
	defer cancel()
	_ = readSSEEvent(t, reader)
	event := readSSEEvent(t, reader)
	if event.comment != "heartbeat" {
		t.Fatalf("event = %#v", event)
	}
}

func TestStreamDomainsCancelsSubscriptionOnDisconnect(t *testing.T) {
	service := &fakeDomainService{updates: make(chan struct{}, 1), cancelled: make(chan struct{})}
	_, reader, cancel := openDomainStream(t, service, "")
	_ = readSSEEvent(t, reader)
	cancel()
	select {
	case <-service.cancelled:
	case <-time.After(time.Second):
		t.Fatal("subscription not cancelled")
	}
}

func TestStreamDomainsRejectsInvalidQueryBeforeStreaming(t *testing.T) {
	service := &fakeDomainService{updates: make(chan struct{}, 1), cancelled: make(chan struct{})}
	h := &DomainHandler{domainService: service}
	r := gin.New()
	r.GET("/stream", h.streamDomains)
	server := httptest.NewServer(r)
	defer server.Close()
	resp, err := http.Get(server.URL + "/stream?pageSize=bad")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("response = %d %#v", resp.StatusCode, resp.Header)
	}
}

func TestStreamDomainsReturnsInitialListErrorBeforeStreaming(t *testing.T) {
	service := &fakeDomainService{
		listErr: errors.New("database unavailable"),
		updates: make(chan struct{}, 1), cancelled: make(chan struct{}),
	}
	h := &DomainHandler{domainService: service}
	r := gin.New()
	r.GET("/stream", h.streamDomains)
	server := httptest.NewServer(r)
	defer server.Close()
	resp, err := http.Get(server.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("response = %d %#v", resp.StatusCode, resp.Header)
	}
}
```

To test heartbeat without a 15-second test, declare:

```go
var domainStreamHeartbeatInterval = 15 * time.Second
```

Tests save old value and restore with `t.Cleanup`.
Do not mark heartbeat test parallel because it temporarily replaces package variable.

- [ ] **Step 6: Run stream tests**

Run: `go test ./internal/application/api/route/domain -run TestStreamDomains -count=1`

Expected: PASS.

- [ ] **Step 7: Run race-sensitive domain tests**

Run: `go test -race ./internal/services/domain ./internal/application/api/route/domain -count=1`

Expected: PASS with no race reports.

- [ ] **Step 8: Commit**

```bash
git add internal/application/api/route/domain
git commit -m "feat(domain): stream list updates over SSE"
```

---

### Task 7: Operator Documentation and Full Verification

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: completed HTTP/config contract.
- Produces: operator instructions for cookie security, dual auth, logout, and SSE.

- [ ] **Step 1: Update environment documentation**

Add table rows:

```markdown
| `CORS_ALLOWED_ORIGIN` | Yes for browser cookie auth | Exact frontend origin used for credentialed CORS and CSRF checks, for example `https://app.example.com`. |
| `AUTH_COOKIE_SECURE` | No | Marks JWT cookie `Secure`. Defaults to `true`; set `false` only for local HTTP development. |
```

Add `CORS_ALLOWED_ORIGIN=http://localhost:3000` and `AUTH_COOKIE_SECURE=false` to local example. State production must use HTTPS and `AUTH_COOKIE_SECURE=true`.

- [ ] **Step 2: Update API documentation**

Add routes:

```markdown
| `POST` | `/api/auth/logout` | Clear browser JWT cookie. |
| `GET` | `/api/domains/stream` | Stream filtered domain-list snapshots through SSE. |
```

Document:

- Login returns JSON token and sets `tunnel_manager_token`.
- Protected routes accept Bearer or cookie.
- Bearer takes precedence.
- Unsafe cookie requests require matching Origin.
- `EventSource("/api/domains/stream?..." )` receives immediate `domains` event and later snapshots.
- SSE data matches list response and heartbeat comments carry no data.
- Broadcaster supports one backend process; shared pub/sub is required before replicas.

- [ ] **Step 3: Format Go files**

Run: `gofmt -w internal/pkg/authcookie/*.go internal/pkg/middleware/*.go internal/application/api/route/auth/*.go internal/application/api/route/domain/*.go internal/services/domain/*.go internal/pkg/config/config.go`

Expected: command exits 0.

- [ ] **Step 4: Run all tests**

Run: `go test ./...`

Expected: PASS for every package, zero failures.

- [ ] **Step 5: Run race tests for changed concurrent/streaming packages**

Run: `go test -race ./internal/services/domain ./internal/application/api/route/domain ./internal/pkg/middleware`

Expected: PASS with no race reports.

- [ ] **Step 6: Build all packages**

Run: `go build ./...`

Expected: exit 0 with no compiler errors.

- [ ] **Step 7: Review diff against acceptance criteria**

Run: `git diff --check && git status --short && git diff --stat HEAD`

Expected: no whitespace errors; only planned source, tests, config example, and README changes remain.

Confirm explicitly:

- Native same-origin `EventSource` authenticates with cookie.
- Bearer clients remain compatible.
- Initial filtered snapshot sends immediately.
- Successful persistence writes notify without blocking.
- Disconnect removes subscriber.
- Unsafe cookie mutations reject missing/mismatched Origin.
- Login/password-change issue cookie; logout clears it.
- No dependency changed.

- [ ] **Step 8: Commit**

```bash
git add README.md
git commit -m "docs: document SSE and cookie authentication"
```
