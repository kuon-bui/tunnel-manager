package domainroute

import (
	"net/http"
	"tunnelmanager/internal/application/api/common"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) deleteDomain(c *gin.Context) {
	if err := h.domainService.DeleteDomain(c.Request.Context(), c.Param("id")); err != nil {
		common.WriteGetErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
