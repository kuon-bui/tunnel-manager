package authroute

import (
	"net/http"

	"tunnelmanager/internal/pkg/authcookie"

	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) logout(c *gin.Context) {
	authcookie.Clear(c.Writer, h.cfg.AuthCookieSecure)
	c.Status(http.StatusNoContent)
}
