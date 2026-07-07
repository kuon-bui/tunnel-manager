package module

import (
	"go.uber.org/fx"

	"tunnelmanager/internal/application/domain"
	"tunnelmanager/internal/application/ports"
	appconfig "tunnelmanager/internal/infrastructure/config"
	"tunnelmanager/internal/portalloc"
	"tunnelmanager/internal/repositories"
)

type domainServiceResult struct {
	fx.Out

	Service    domain.DomainService
	Reconciler Reconciler
}

func NewDomainService(
	repo repositories.DomainRepository,
	cf ports.CloudflareClient,
	supervisor ports.ProcessSupervisor,
	allocator *portalloc.Allocator,
	cfg appconfig.Config,
) (domainServiceResult, error) {
	service := domain.NewDomainService(repo, cf, supervisor, allocator, cfg.EncryptionKey, cfg.LogDir)

	return domainServiceResult{
		Service:    service,
		Reconciler: service,
	}, nil
}

var Application = fx.Module("application",
	fx.Provide(NewDomainService),
	fx.Invoke(func(supervisor ports.ProcessSupervisor, svc domain.DomainService) {
		supervisor.SetEventHandler(svc.HandleSupervisorEvent)
	}),
)
