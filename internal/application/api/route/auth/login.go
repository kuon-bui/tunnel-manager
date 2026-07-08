package authroute

import (
	"errors"
	"net/http"

	authrequest "tunnelmanager/internal/pkg/request/auth"
	authservice "tunnelmanager/internal/services/auth"

	"github.com/gin-gonic/gin"
)

func (h *AuthHandler) login(c *gin.Context) {
	var req authrequest.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	token, expiresAt, err := h.authService.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, authservice.ErrInvalidCredentials) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token":     token,
		"expiresAt": expiresAt,
	})
}
