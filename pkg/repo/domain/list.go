package domainrepo

import (
	"context"
	"tunnelmanager/internal/model"
)

func (r *Repository) List(ctx context.Context) ([]model.Domain, error) {
	var domains []model.Domain
	if err := r.db.NewSelect().Model(&domains).Order("created_at ASC").Scan(ctx); err != nil {
		return nil, err
	}

	return domains, nil
}

func (r *Repository) ListByStatus(ctx context.Context, status model.Status) ([]model.Domain, error) {
	var domains []model.Domain
	if err := r.db.NewSelect().Model(&domains).Where("status = ?", status).Order("created_at ASC").Scan(ctx); err != nil {
		return nil, err
	}

	return domains, nil
}
