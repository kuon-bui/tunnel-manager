package module

import (
	"github.com/uptrace/bun"
	"go.uber.org/fx"

	persistencesqlite "tunnelmanager/internal/infrastructure/persistence/sqlite"
	"tunnelmanager/internal/repositories"
)

func NewDomainRepository(db *bun.DB) repositories.DomainRepository {
	return persistencesqlite.NewRepository(db)
}

var Repositories = fx.Module("repositories",
	fx.Provide(NewDomainRepository),
)
