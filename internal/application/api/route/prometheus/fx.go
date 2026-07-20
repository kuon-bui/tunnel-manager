package prometheusroute

import (
	"tunnelmanager/internal/pkg/common"

	"go.uber.org/fx"
)

var Module = fx.Module(
	"prometheus-route",
	common.ProvideAsRoute(NewPrometheusRoute),
	fx.Provide(NewPrometheusRouteHandler),
)
