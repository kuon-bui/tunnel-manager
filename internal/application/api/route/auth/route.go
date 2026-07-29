package authroute

import (
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/middleware"
	authservice "tunnelmanager/internal/services/auth"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

type AuthRoute struct {
	*gin.Engine
	authHandler *AuthHandler
	authService authservice.AuthService
	cfg         config.Config
}

type AuthRouteParams struct {
	fx.In

	Engine      *gin.Engine
	AuthHandler *AuthHandler
	AuthService authservice.AuthService
	Cfg         config.Config
}

func NewAuthRoute(params AuthRouteParams) *AuthRoute {
	return &AuthRoute{
		Engine:      params.Engine,
		authHandler: params.AuthHandler,
		authService: params.AuthService,
		cfg:         params.Cfg,
	}
}

func (r *AuthRoute) Setup() {
	g := r.Group("/api/auth")
	g.POST("/login", middleware.RequireAllowedOrigin(r.cfg), r.authHandler.login)
	g.POST("/logout", middleware.RequireAllowedOrigin(r.cfg), r.authHandler.logout)
	g.PUT("/password", middleware.JWTAuth(r.authService, r.cfg), r.authHandler.changePassword)
}
