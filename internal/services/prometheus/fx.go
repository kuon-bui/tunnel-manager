package prometheusserivce

import "go.uber.org/fx"

var Module = fx.Module(
	"prometheus-service",
	fx.Provide(NewPrometheusService),
)
