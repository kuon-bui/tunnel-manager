package application

import (
	"tunnelmanager/internal/services"
	"tunnelmanager/pkg/cloudflare"
	"tunnelmanager/pkg/config"
	"tunnelmanager/pkg/portalloc"
	"tunnelmanager/pkg/process"
	"tunnelmanager/pkg/repo"
	"tunnelmanager/pkg/sqlite"

	"go.uber.org/fx"
)

var Module = fx.Module(
	"application",
	config.Module,
	sqlite.Module,
	cloudflare.Module,
	portalloc.Module,
	process.Module,
	repo.Module,
	services.Module,
)
