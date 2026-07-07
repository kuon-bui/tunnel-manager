package domainroute

import (
	"net/http"
	domainrequest "tunnelmanager/pkg/request/domain"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) createDomain(c *gin.Context) {
	var req domainrequest.CreateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	domain, err := h.domainService.CreateDomain(c.Request.Context(), req.Hostname, req.OriginURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, domain)
}
