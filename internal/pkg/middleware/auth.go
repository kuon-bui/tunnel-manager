package middleware

import (
	"context"
	"net/http"
	"strings"
	"tunnelmanager/internal/pkg/authcookie"
	"tunnelmanager/internal/pkg/config"

	"github.com/gin-gonic/gin"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (string, error)
}

const (
	AuthenticatedUsernameKey   = "authenticatedUsername"
	AuthenticationSourceKey    = "authenticationSource"
	AuthenticationSourceBearer = "bearer"
	AuthenticationSourceCookie = "cookie"
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
