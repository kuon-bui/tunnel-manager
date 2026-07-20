package prometheusroute

import (
	"tunnelmanager/internal/pkg/config"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

type PrometheusRoute struct {
	*gin.Engine
	prometheusHandler *PrometheusRouteHandler
	jwtSecret         []byte
}

type PrometheusRouteParams struct {
	fx.In
	Engine            *gin.Engine
	PrometheusHandler *PrometheusRouteHandler
	Cfg               config.Config
}

func NewPrometheusRoute(params PrometheusRouteParams) *PrometheusRoute {
	return &PrometheusRoute{
		Engine:            params.Engine,
		prometheusHandler: params.PrometheusHandler,
		jwtSecret:         params.Cfg.JWTSecret,
	}
}

func (r *PrometheusRoute) Setup() {
	g := r.Group("/api/prometheus")
	g.GET("/discovery", r.prometheusHandler.discovery)
}
