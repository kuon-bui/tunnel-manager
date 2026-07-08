package authservice

import (
	"tunnelmanager/internal/pkg/lifecycle"

	"go.uber.org/fx"
)

func AsBootstrapper(service AuthService) lifecycle.Bootstrapper {
	return service
}

var Module = fx.Module("authservice",
	fx.Provide(
		NewAuthService,
		fx.Annotate(
			AsBootstrapper,
			fx.ResultTags(`group:"bootstrappers"`),
		),
	),
)
