package api

import (
	"tunnelmanager/internal/application/api/server"

	"go.uber.org/fx"
)

var Module = fx.Module(
	"api",
	server.Module,
)
