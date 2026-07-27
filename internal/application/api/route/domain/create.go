package domainroute

import (
	"errors"
	"net/http"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
	domainservice "tunnelmanager/internal/services/domain"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) createDomain(c *gin.Context) {
	var req domainrequest.CreateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	domain, err := h.domainService.CreateDomain(c.Request.Context(), req.Hostname, req.OriginURL, req.ZoneID)
	if err != nil {
		if errors.Is(err, domainservice.ErrCloudflareUnavailable) {
			c.JSON(http.StatusBadGateway, gin.H{"error": "Cloudflare unavailable"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, domain)
}
