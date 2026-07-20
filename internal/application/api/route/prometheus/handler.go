package prometheusroute

import (
	prometheusserivce "tunnelmanager/internal/services/prometheus"

	"go.uber.org/fx"
)

type PrometheusRouteHandler struct {
	prometheusService prometheusserivce.PrometheusService
}

type PrometheusRouteHandlerParams struct {
	fx.In
	PrometheusService prometheusserivce.PrometheusService
}

func NewPrometheusRouteHandler(params PrometheusRouteHandlerParams) *PrometheusRouteHandler {
	return &PrometheusRouteHandler{
		prometheusService: params.PrometheusService,
	}
}
