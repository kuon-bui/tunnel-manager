package repo

import (
	authrepo "tunnelmanager/internal/pkg/repo/auth"
	domainrepo "tunnelmanager/internal/pkg/repo/domain"

	"go.uber.org/fx"
)

var Module = fx.Module("repo",
	fx.Provide(
		domainrepo.NewRepository,
		authrepo.NewRepository,
	),
)
