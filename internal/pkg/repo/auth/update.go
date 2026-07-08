package authrepo

import (
	"context"
	"tunnelmanager/internal/model"
)

func (r *authRepository) Update(ctx context.Context, auth *model.Auth) error {
	result, err := r.db.NewUpdate().Model(auth).WherePK().Exec(ctx)
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
