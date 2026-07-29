package middleware

import (
	"net/http"

	"tunnelmanager/internal/pkg/config"

	"github.com/gin-gonic/gin"
)

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
