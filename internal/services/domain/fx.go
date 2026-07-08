package domainservice

import (
	"tunnelmanager/internal/pkg/lifecycle"

	"go.uber.org/fx"
)

func AsReconciler(service DomainService) lifecycle.Reconciler {
	return service
}

var Module = fx.Module("domainservice",
	fx.Provide(
		NewDomainService,
		AsReconciler,
	),
)
