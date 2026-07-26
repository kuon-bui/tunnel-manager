package domainservice

import (
	"context"

	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/lifecycle"

	"go.uber.org/fx"
)

func AsReconciler(service DomainService) lifecycle.Reconciler {
	return service
}

func RegisterCloudflareSyncScheduler(
	lc fx.Lifecycle,
	cfg config.Config,
	service DomainService,
) {
	scheduler := newCloudflareSyncScheduler(cfg.CloudflareSyncInterval, service.SyncCloudflare)
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			scheduler.Start()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return scheduler.Stop(ctx)
		},
	})
}

var Module = fx.Module("domainservice",
	fx.Provide(
		NewDomainService,
		AsReconciler,
	),
	fx.Invoke(RegisterCloudflareSyncScheduler),
)
