package prometheusroute

import "github.com/gin-gonic/gin"

func (h *PrometheusRouteHandler) discovery(c *gin.Context) {
	res, err := h.prometheusService.Discovery(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, res)
}
