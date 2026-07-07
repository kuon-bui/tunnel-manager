package module

import (
	"go.uber.org/fx"

	appconfig "tunnelmanager/internal/infrastructure/config"
)

var Config = fx.Module("config",
	fx.Provide(appconfig.Load),
)
