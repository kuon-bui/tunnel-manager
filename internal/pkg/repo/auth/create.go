package authrepo

import (
	"context"
	"tunnelmanager/internal/model"
)

func (r *authRepository) Create(ctx context.Context, auth *model.Auth) error {
	_, err := r.db.NewInsert().Model(auth).Exec(ctx)
	return err
}
