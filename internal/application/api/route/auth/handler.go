package authroute

import (
	"tunnelmanager/internal/pkg/config"
	authservice "tunnelmanager/internal/services/auth"

	"go.uber.org/fx"
)

type AuthHandler struct {
	authService authservice.AuthService
	cfg         config.Config
}

type AuthHandlerParams struct {
	fx.In

	AuthService authservice.AuthService
	Cfg         config.Config
}

func NewAuthHandler(params AuthHandlerParams) *AuthHandler {
	return &AuthHandler{
		authService: params.AuthService,
		cfg:         params.Cfg,
	}
}
