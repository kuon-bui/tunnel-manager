package authrepo

import (
	"context"
	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/repo/helpers"
)

func (r *authRepository) GetByUsername(ctx context.Context, username string) (*model.Auth, error) {
	row := new(model.Auth)
	if err := r.db.NewSelect().Model(row).Where("username = ?", username).Scan(ctx); err != nil {
		return nil, helpers.MapNotFound(err)
	}
	return row, nil
}
