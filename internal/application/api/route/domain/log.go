package domainroute

import (
	"net/http"
	"tunnelmanager/internal/application/api/common"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) getLogs(c *gin.Context) {
	lines, err := h.domainService.Logs(c.Request.Context(), c.Param("id"))
	if err != nil {
		common.WriteGetErr(c, err)
		return
	}
	c.JSON(http.StatusOK, lines)
}

func (h *DomainHandler) getMetrics(c *gin.Context) {
	if err := h.domainService.ProxyMetrics(c.Request.Context(), c.Param("id"), c.Writer); err != nil {
		common.WriteGetErr(c, err)
		return
	}
}
