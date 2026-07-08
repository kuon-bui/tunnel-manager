package gin

import (
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/middleware"

	"github.com/gin-gonic/gin"
)

func NewGinEngine(config config.Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(middleware.CorsMiddleware(config.CORSAllowedOrigin))

	return r
}
