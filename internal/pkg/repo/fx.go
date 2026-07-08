package repo

import (
	domainrepo "tunnelmanager/internal/pkg/repo/domain"

	"go.uber.org/fx"
)

var Module = fx.Module("repo",
	fx.Provide(
		domainrepo.NewRepository,
	),
)
