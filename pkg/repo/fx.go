package repo

import (
	domainrepo "tunnelmanager/pkg/repo/domain"

	"go.uber.org/fx"
)

func AsDomainRepository(repo *domainrepo.Repository) domainrepo.DomainRepository {
	return repo
}

var Module = fx.Module("repo",
	fx.Provide(
		domainrepo.NewRepository,
		AsDomainRepository,
	),
)
