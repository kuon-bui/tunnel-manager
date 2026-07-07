package domainservice

import (
	"tunnelmanager/pkg/lifecycle"

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
