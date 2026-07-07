package domainroute

import (
	"net/http"
	"tunnelmanager/internal/application/api/common"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) getDomain(c *gin.Context) {
	domain, err := h.domainService.GetDomain(c.Request.Context(), c.Param("id"))
	if err != nil {
		common.WriteGetErr(c, err)
		return
	}
	c.JSON(http.StatusOK, domain)
}
