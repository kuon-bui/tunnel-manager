package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (string, error)
}

const AuthenticatedUsernameKey = "authenticatedUsername"

func JWTAuth(authenticator Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		token, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || token == "" || strings.ContainsAny(token, " \t\r\n") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		username, err := authenticator.Authenticate(c.Request.Context(), token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		c.Set(AuthenticatedUsernameKey, username)
		c.Next()
	}
}
