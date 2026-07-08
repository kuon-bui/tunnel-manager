package domainrepo

import (
	"context"
	"tunnelmanager/internal/model"
)

func (r *domainRepository) Update(ctx context.Context, domain *model.Domain) error {
	result, err := r.db.NewUpdate().Model(domain).WherePK().Exec(ctx)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return model.ErrNotFound
	}

	return nil
}

func (r *domainRepository) UpdateBulk(ctx context.Context, domains []*model.Domain) error {
	if len(domains) == 0 {
		return nil
	}

	_, err := r.db.NewUpdate().Model(&domains).Bulk().Exec(ctx)
	return err
}
