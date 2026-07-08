package authroute

import (
	"tunnelmanager/internal/pkg/common"

	"go.uber.org/fx"
)

var Module = fx.Module(
	"auth-route",
	common.ProvideAsRoute(NewAuthRoute),
	fx.Provide(NewAuthHandler),
)
