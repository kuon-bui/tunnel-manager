package authrepo

import "github.com/uptrace/bun"

type AuthRepository interface {
}
type authRepository struct {
	db *bun.DB
}

func NewRepository(db *bun.DB) AuthRepository {
	return &authRepository{db: db}
}
