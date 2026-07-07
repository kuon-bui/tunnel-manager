package http

import (
	"tunnelmanager/internal/application/domain"

	"github.com/gin-gonic/gin"
)

func NewRouter(svc domain.DomainService) *gin.Engine {
	h := &handlers{svc: svc}
	r := gin.New()
	r.Use(gin.Recovery())

	g := r.Group("/api/domains")
	g.POST("", h.createDomain)
	g.GET("", h.listDomains)
	g.GET("/:id", h.getDomain)
	g.PUT("/:id", h.updateDomain)
	g.DELETE("/:id", h.deleteDomain)
	g.POST("/:id/stop", h.stopDomain)
	g.POST("/:id/restart", h.restartDomain)
	g.GET("/:id/logs", h.getLogs)
	g.GET("/:id/metrics", h.getMetrics)

	return r
}
