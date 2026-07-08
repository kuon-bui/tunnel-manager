package application

import (
	"tunnelmanager/internal/pkg/cloudflare"
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/portalloc"
	"tunnelmanager/internal/pkg/process"
	"tunnelmanager/internal/pkg/repo"
	"tunnelmanager/internal/pkg/sqlite"
	"tunnelmanager/internal/services"

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
