package domainroute

import (
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/middleware"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

type DomainRoute struct {
	*gin.Engine
	domainHandler *DomainHandler
	jwtSecret     []byte
}

type DomainRouteParams struct {
	fx.In

	Engine        *gin.Engine
	DomainHandler *DomainHandler
	Cfg           config.Config
}

func NewDomainRoute(params DomainRouteParams) *DomainRoute {
	return &DomainRoute{
		Engine:        params.Engine,
		domainHandler: params.DomainHandler,
		jwtSecret:     params.Cfg.JWTSecret,
	}
}

func (r *DomainRoute) Setup() {

	g := r.Group("/api/domains", middleware.JWTAuth(r.jwtSecret))
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
