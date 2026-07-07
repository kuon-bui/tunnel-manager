package domainroute

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) listDomains(c *gin.Context) {
	domains, err := h.domainService.ListDomains(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, domains)
}
