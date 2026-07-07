package domainroute

import (
	"net/http"
	"tunnelmanager/internal/application/api/common"
	domainrequest "tunnelmanager/pkg/request/domain"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) updateDomain(c *gin.Context) {
	var req domainrequest.UpdateDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	id := c.Param("id")
	domain, err := h.domainService.UpdateOrigin(c.Request.Context(), id, req.OriginURL)
	if err != nil {
		common.WriteGetErr(c, err)
		return
	}
	c.JSON(http.StatusOK, domain)
}
