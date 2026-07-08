package domainroute

import (
	"net/http"
	domainrequest "tunnelmanager/internal/pkg/request/domain"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) listDomains(c *gin.Context) {
	var req domainrequest.ListDomainRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	domains, nextCursor, err := h.domainService.ListDomains(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": domains, "nextCursor": nextCursor})
}
