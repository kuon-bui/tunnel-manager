package process

import "go.uber.org/fx"

var Module = fx.Module("process",
	fx.Provide(
		NewSupervisor,
	),
)
