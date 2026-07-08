package authservice

import (
	"context"
	"time"

	"tunnelmanager/internal/pkg/config"
	authrepo "tunnelmanager/internal/pkg/repo/auth"

	"go.uber.org/fx"
)

type AuthService interface {
	Login(ctx context.Context, username, password string) (token string, expiresAt time.Time, err error)
	Bootstrap(ctx context.Context) error
}

type authService struct {
	repo          authrepo.AuthRepository
	secret        []byte
	ttl           time.Duration
	adminUsername string
	adminPassword string
}

type AuthServiceParams struct {
	fx.In

	Cfg  config.Config
	Repo authrepo.AuthRepository
}

func NewAuthService(params AuthServiceParams) AuthService {
	return &authService{
		repo:          params.Repo,
		secret:        params.Cfg.JWTSecret,
		ttl:           params.Cfg.JWTTTL,
		adminUsername: params.Cfg.AdminUsername,
		adminPassword: params.Cfg.AdminPassword,
	}
}
