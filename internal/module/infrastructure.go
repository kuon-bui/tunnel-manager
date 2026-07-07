package module

import (
	"go.uber.org/fx"

	"tunnelmanager/internal/application/ports"
	cloudflareadapter "tunnelmanager/internal/infrastructure/cloudflare"
	appconfig "tunnelmanager/internal/infrastructure/config"
	processadapter "tunnelmanager/internal/infrastructure/process"
	"tunnelmanager/internal/portalloc"
)

func NewCloudflareClient(cfg appconfig.Config) ports.CloudflareClient {
	return cloudflareadapter.New(cfg.CloudflareAPIToken, cfg.CloudflareAccountID, cfg.CloudflareZoneID)
}

func NewSupervisor(cfg appconfig.Config) ports.ProcessSupervisor {
	return processadapter.New(cfg.CloudflaredBinary, cfg.CloudflaredProtocol, nil)
}

func NewPortAllocator(cfg appconfig.Config) *portalloc.Allocator {
	return portalloc.NewAllocator(cfg.MetricsPortRangeStart, cfg.MetricsPortRangeEnd)
}

var Infrastructure = fx.Module("infrastructure",
	fx.Provide(
		NewCloudflareClient,
		NewSupervisor,
		NewPortAllocator,
	),
)
