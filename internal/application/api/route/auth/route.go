package authroute

import (
	"tunnelmanager/internal/pkg/middleware"
	authservice "tunnelmanager/internal/services/auth"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

type AuthRoute struct {
	*gin.Engine
	authHandler *AuthHandler
	authService authservice.AuthService
}

type AuthRouteParams struct {
	fx.In

	Engine      *gin.Engine
	AuthHandler *AuthHandler
	AuthService authservice.AuthService
}

func NewAuthRoute(params AuthRouteParams) *AuthRoute {
	return &AuthRoute{
		Engine:      params.Engine,
		authHandler: params.AuthHandler,
		authService: params.AuthService,
	}
}

func (r *AuthRoute) Setup() {
	g := r.Group("/api/auth")
	g.POST("/login", r.authHandler.login)
	g.PUT("/password", middleware.JWTAuth(r.authService), r.authHandler.changePassword)
}
