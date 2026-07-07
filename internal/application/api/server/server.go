package server

import (
	"net/http"
	"tunnelmanager/pkg/config"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

type NewHttpServerParams struct {
	fx.In

	Cfg    config.Config
	Router *gin.Engine
}

func NewHTTPServer(params NewHttpServerParams) *http.Server {
	return &http.Server{
		Addr:    params.Cfg.HTTPAddr,
		Handler: params.Router,
	}
}
