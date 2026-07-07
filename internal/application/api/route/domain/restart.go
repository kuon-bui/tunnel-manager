package domainroute

import (
	"net/http"
	"tunnelmanager/internal/application/api/common"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) restartDomain(c *gin.Context) {
	id := c.Param("id")
	if err := h.domainService.RestartDomain(c.Request.Context(), id); err != nil {
		common.WriteGetErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
