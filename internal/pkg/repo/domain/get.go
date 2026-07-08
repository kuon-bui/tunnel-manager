package domainrepo

import (
	"context"
	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/repo/helpers"
)

func (r *domainRepository) Get(ctx context.Context, id string) (*model.Domain, error) {
	row := new(model.Domain)
	if err := r.db.NewSelect().Model(row).Where("id = ?", id).Scan(ctx); err != nil {
		return nil, helpers.MapNotFound(err)
	}
	return row, nil
}

func (r *domainRepository) GetByHostname(ctx context.Context, hostname string) (*model.Domain, error) {
	row := new(model.Domain)
	if err := r.db.NewSelect().Model(row).Where("hostname = ?", hostname).Scan(ctx); err != nil {
		return nil, helpers.MapNotFound(err)
	}
	return row, nil
}
