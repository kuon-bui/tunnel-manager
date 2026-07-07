package server

import (
	"tunnelmanager/internal/application/api/route"
	"tunnelmanager/pkg/gin"
	"tunnelmanager/pkg/lifecycle"

	"go.uber.org/fx"
)

var Module = fx.Module("server",
	route.Module,
	gin.Module,
	fx.Provide(NewHTTPServer),
	lifecycle.Module,
)
