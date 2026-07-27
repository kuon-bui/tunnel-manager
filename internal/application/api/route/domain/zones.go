package domainroute

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) listCloudflareZones(c *gin.Context) {
	zones, err := h.domainService.ListCloudflareZones(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "Cloudflare unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": zones})
}
