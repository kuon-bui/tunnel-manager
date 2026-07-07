package domainroute

import (
	"net/http"
	"tunnelmanager/internal/application/api/common"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) stopDomain(c *gin.Context) {
	if err := h.domainService.StopDomain(c.Request.Context(), c.Param("id")); err != nil {
		common.WriteGetErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
