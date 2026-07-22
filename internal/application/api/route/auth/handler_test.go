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
