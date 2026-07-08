package portalloc

import "go.uber.org/fx"

var Module = fx.Module("portalloc",
	fx.Provide(
		NewPortAllocator,
	),
)
