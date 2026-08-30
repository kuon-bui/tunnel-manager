package domainroute

import (
	"errors"
	"net/http"

	"tunnelmanager/internal/model"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
	domainservice "tunnelmanager/internal/services/domain"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) replaceRoutes(c *gin.Context) {
	var req domainrequest.ReplaceRoutesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	domain, err := h.domainService.ReplaceRoutes(c.Request.Context(), c.Param("id"), req.Routes)
	if err != nil {
		switch {
		case errors.Is(err, domainservice.ErrInvalidRoutes):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, domainservice.ErrCloudflareUnavailable):
			c.JSON(http.StatusBadGateway, gin.H{"error": "Cloudflare unavailable"})
		case errors.Is(err, model.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "domain not found"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		}
		return
	}
	c.JSON(http.StatusOK, domain)
}
