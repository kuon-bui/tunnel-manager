package domainrepo

import (
	"context"
	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/constant"
	domainrequest "tunnelmanager/internal/pkg/request/domain"

	"github.com/uptrace/bun"
)

type DomainRepository interface {
	Create(ctx context.Context, domain *model.Domain) error
	List(ctx context.Context, req domainrequest.ListDomainRequest) ([]*model.Domain, string, error)
	ListAll(ctx context.Context, statuses ...constant.DomainStatus) ([]*model.Domain, error)
	Get(ctx context.Context, id string) (*model.Domain, error)
	GetByHostname(ctx context.Context, hostname string) (*model.Domain, error)
	Update(ctx context.Context, domain *model.Domain) error
	UpdateBulk(ctx context.Context, domains []*model.Domain) error
	Delete(ctx context.Context, id string) error
	ListTakenPorts(ctx context.Context) (map[int]bool, error)
}

type domainRepository struct {
	db *bun.DB
}

func NewRepository(db *bun.DB) DomainRepository {
	return &domainRepository{db: db}
}
