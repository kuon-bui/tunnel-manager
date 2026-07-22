package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"tunnelmanager/internal/pkg/authcookie"
	"tunnelmanager/internal/pkg/config"

	"github.com/gin-gonic/gin"
)

type fakeAuthenticator struct {
	username string
	err      error
	token    string
}

func (f *fakeAuthenticator) Authenticate(_ context.Context, token string) (string, error) {
	f.token = token
	return f.username, f.err
}

func TestJWTAuthRejectsMissingOrMalformedBearer(t *testing.T) {
	for _, header := range []string{"", "Token abc", "Bearer ", "Bearer abc extra"} {
		t.Run(header, func(t *testing.T) {
			status, _ := authenticatedRequest(t, http.MethodGet, header, "", "", &fakeAuthenticator{username: "admin"})
			if status != http.StatusUnauthorized {
				t.Fatalf("status = %d", status)
			}
		})
	}
}

func TestJWTAuthRejectsAuthenticatorError(t *testing.T) {
	status, _ := authenticatedRequest(t, http.MethodGet, "Bearer abc", "", "", &fakeAuthenticator{err: errors.New("invalid")})
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d", status)
	}
}

func TestJWTAuthStoresAuthenticatedUsername(t *testing.T) {
	authenticator := &fakeAuthenticator{username: "admin"}
	status, username := authenticatedRequest(t, http.MethodGet, "Bearer abc", "", "", authenticator)
	if status != http.StatusOK || username != "admin" || authenticator.token != "abc" {
		t.Fatalf("status/username/token = %d/%q/%q", status, username, authenticator.token)
	}
}

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

func authenticatedRequest(t *testing.T, method, header, cookieToken, origin string, authenticator Authenticator) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var username string
	router.Handle(method, "/private", JWTAuth(authenticator, config.Config{CORSAllowedOrigin: "https://app.example.com"}), func(c *gin.Context) {
		username = c.GetString(AuthenticatedUsernameKey)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(method, "/private", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	if cookieToken != "" {
		req.AddCookie(&http.Cookie{Name: authcookie.Name, Value: cookieToken})
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res.Code, username
}
