package domainrepo

import (
	"context"
	"tunnelmanager/internal/model"

	"github.com/uptrace/bun"
)

type DomainRepository interface {
	Create(ctx context.Context, domain *model.Domain) error
	List(ctx context.Context) ([]model.Domain, error)
	Get(ctx context.Context, id string) (*model.Domain, error)
	GetByHostname(ctx context.Context, hostname string) (*model.Domain, error)
	ListByStatus(ctx context.Context, status model.Status) ([]model.Domain, error)
	Update(ctx context.Context, domain *model.Domain) error
	Delete(ctx context.Context, id string) error
}

type Repository struct {
	db *bun.DB
}

func NewRepository(db *bun.DB) *Repository {
	return &Repository{db: db}
}
