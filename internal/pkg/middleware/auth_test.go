package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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
			status, _ := authenticatedRequest(t, header, &fakeAuthenticator{username: "admin"})
			if status != http.StatusUnauthorized {
				t.Fatalf("status = %d", status)
			}
		})
	}
}

func TestJWTAuthRejectsAuthenticatorError(t *testing.T) {
	status, _ := authenticatedRequest(t, "Bearer abc", &fakeAuthenticator{err: errors.New("invalid")})
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d", status)
	}
}

func TestJWTAuthStoresAuthenticatedUsername(t *testing.T) {
	authenticator := &fakeAuthenticator{username: "admin"}
	status, username := authenticatedRequest(t, "Bearer abc", authenticator)
	if status != http.StatusOK || username != "admin" || authenticator.token != "abc" {
		t.Fatalf("status/username/token = %d/%q/%q", status, username, authenticator.token)
	}
}

func authenticatedRequest(t *testing.T, header string, authenticator Authenticator) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	var username string
	router.GET("/private", JWTAuth(authenticator), func(c *gin.Context) {
		username = c.GetString(AuthenticatedUsernameKey)
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/private", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)
	return res.Code, username
}
