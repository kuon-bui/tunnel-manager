package domainroute

import (
	"errors"
	"net/http"

	"tunnelmanager/internal/application/api/common"
	domainservice "tunnelmanager/internal/services/domain"

	"github.com/gin-gonic/gin"
)

func (h *DomainHandler) startDomain(c *gin.Context) {
	err := h.domainService.StartDomain(c.Request.Context(), c.Param("id"))
	if errors.Is(err, domainservice.ErrAlreadyRunning) {
		c.JSON(http.StatusConflict, gin.H{"error": "tunnel is already running"})
		return
	}
	if err != nil {
		common.WriteGetErr(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
