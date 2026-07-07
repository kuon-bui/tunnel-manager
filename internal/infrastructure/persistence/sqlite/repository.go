package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uptrace/bun"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/repositories"
)

type Repository struct {
	db *bun.DB
}

var _ repositories.DomainRepository = (*Repository)(nil)

func NewRepository(db *bun.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, domain *model.Domain) error {
	_, err := r.db.NewInsert().Model(domain).Exec(ctx)
	return err
}

func (r *Repository) List(ctx context.Context) ([]model.Domain, error) {
	var domains []model.Domain
	if err := r.db.NewSelect().Model(&domains).Order("created_at ASC").Scan(ctx); err != nil {
		return nil, err
	}

	return domains, nil
}

func (r *Repository) Get(ctx context.Context, id string) (*model.Domain, error) {
	row := new(model.Domain)
	if err := r.db.NewSelect().Model(row).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, mapNotFound(err)
	}
	return row, nil
}

func (r *Repository) GetByHostname(ctx context.Context, hostname string) (*model.Domain, error) {
	row := new(model.Domain)
	if err := r.db.NewSelect().Model(row).Where("hostname = ?", hostname).Scan(ctx); err != nil {
		return nil, mapNotFound(err)
	}
	return row, nil
}

func (r *Repository) ListByStatus(ctx context.Context, status model.Status) ([]model.Domain, error) {
	var domains []model.Domain
	if err := r.db.NewSelect().Model(&domains).Where("status = ?", status).Order("created_at ASC").Scan(ctx); err != nil {
		return nil, err
	}

	return domains, nil
}

func (r *Repository) Update(ctx context.Context, domain *model.Domain) error {
	domain.UpdatedAt = time.Now().UTC()
	_, err := r.db.NewUpdate().Model(domain).WherePK().Exec(ctx)
	return err
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	_, err := r.db.NewDelete().Model((*model.Domain)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrNotFound
	}
	return err
}
