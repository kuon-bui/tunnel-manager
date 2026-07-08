package domainrepo

import (
	"context"
	"tunnelmanager/internal/model"
)

func (r *domainRepository) Delete(ctx context.Context, id string) error {
	result, err := r.db.NewDelete().Model((*model.Domain)(nil)).Where("id = ?", id).Exec(ctx)
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
