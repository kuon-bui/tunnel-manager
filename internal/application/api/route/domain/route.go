package domainroute

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

type DomainRoute struct {
	*gin.Engine
	domainHandler *DomainHandler
}

type DomainRouteParams struct {
	fx.In

	Engine        *gin.Engine
	DomainHandler *DomainHandler
}

func NewDomainRoute(params DomainRouteParams) *DomainRoute {
	return &DomainRoute{
		Engine:        params.Engine,
		domainHandler: params.DomainHandler,
	}
}

func (r *DomainRoute) Setup() {

	g := r.Group("/api/domains")
	g.POST("", r.domainHandler.createDomain)
	g.GET("", r.domainHandler.listDomains)
	g.GET("/:id", r.domainHandler.getDomain)
	g.PUT("/:id", r.domainHandler.updateDomain)
	g.DELETE("/:id", r.domainHandler.deleteDomain)
	g.POST("/:id/stop", r.domainHandler.stopDomain)
	g.POST("/:id/restart", r.domainHandler.restartDomain)
	g.GET("/:id/logs", r.domainHandler.getLogs)
	g.GET("/:id/metrics", r.domainHandler.getMetrics)

}
