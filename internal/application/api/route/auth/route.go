package authroute

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

type AuthRoute struct {
	*gin.Engine
	authHandler *AuthHandler
}

type AuthRouteParams struct {
	fx.In

	Engine      *gin.Engine
	AuthHandler *AuthHandler
}

func NewAuthRoute(params AuthRouteParams) *AuthRoute {
	return &AuthRoute{
		Engine:      params.Engine,
		authHandler: params.AuthHandler,
	}
}

func (r *AuthRoute) Setup() {
	g := r.Group("/api/auth")
	g.POST("/login", r.authHandler.login)
}
