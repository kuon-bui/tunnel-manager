package authrepo

import (
	"context"
	"tunnelmanager/internal/model"

	"github.com/uptrace/bun"
)

type AuthRepository interface {
	GetByUsername(ctx context.Context, username string) (*model.Auth, error)
	Create(ctx context.Context, auth *model.Auth) error
	Update(ctx context.Context, auth *model.Auth) error
}

type authRepository struct {
	db *bun.DB
}

func NewRepository(db *bun.DB) AuthRepository {
	return &authRepository{db: db}
}
