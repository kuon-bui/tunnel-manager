package gin

import "go.uber.org/fx"

var Module = fx.Module("gin",
	fx.Provide(
		NewGinEngine,
	),
)
