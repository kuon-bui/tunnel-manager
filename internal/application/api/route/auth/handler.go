package authroute

import (
	authservice "tunnelmanager/internal/services/auth"

	"go.uber.org/fx"
)

type AuthHandler struct {
	authService authservice.AuthService
}

type AuthHandlerParams struct {
	fx.In

	AuthService authservice.AuthService
}

func NewAuthHandler(params AuthHandlerParams) *AuthHandler {
	return &AuthHandler{
		authService: params.AuthService,
	}
}
