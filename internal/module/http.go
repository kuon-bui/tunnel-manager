package module

import (
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"

	"tunnelmanager/internal/application/domain"
	appconfig "tunnelmanager/internal/infrastructure/config"
	httpinterface "tunnelmanager/internal/interfaces/http"
)

func NewRouter(svc domain.DomainService) *gin.Engine {
	return httpinterface.NewRouter(svc)
}

func NewHTTPServer(cfg appconfig.Config, router *gin.Engine) *stdhttp.Server {
	return &stdhttp.Server{
		Addr:    cfg.HTTPAddr,
		Handler: router,
	}
}

var HTTP = fx.Module("http",
	fx.Provide(
		NewRouter,
		NewHTTPServer,
	),
)
