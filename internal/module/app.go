package module

import "go.uber.org/fx"

var App = fx.Options(
	Config,
	Database,
	Repositories,
	Infrastructure,
	Application,
	HTTP,
	Lifecycle,
)
