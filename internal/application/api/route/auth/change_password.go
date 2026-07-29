package authroute

import (
	"errors"
	"net/http"

	"tunnelmanager/internal/pkg/authcookie"
	"tunnelmanager/internal/pkg/middleware"
	authrequest "tunnelmanager/internal/pkg/request/auth"
	authservice "tunnelmanager/internal/services/auth"

	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) changePassword(c *gin.Context) {
	var req authrequest.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, expiresAt, err := h.authService.ChangePassword(
		c.Request.Context(),
		c.GetString(middleware.AuthenticatedUsernameKey),
		req.CurrentPassword,
		req.NewPassword,
	)
	if err != nil {
		switch {
		case errors.Is(err, authservice.ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		case errors.Is(err, authservice.ErrInvalidPassword), errors.Is(err, authservice.ErrSamePassword):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}

	authcookie.Set(c.Writer, token, expiresAt, h.cfg.AuthCookieSecure)
	c.JSON(http.StatusOK, gin.H{"token": token, "expiresAt": expiresAt})
}
