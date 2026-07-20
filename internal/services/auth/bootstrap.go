package authservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"tunnelmanager/internal/model"

	"golang.org/x/crypto/bcrypt"
)

func (s *authService) Bootstrap(ctx context.Context) error {
	_, err := s.repo.GetByUsername(ctx, s.adminUsername)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return fmt.Errorf("authservice: bootstrap: get user: %w", err)
	}

	if errors.Is(err, model.ErrNotFound) {
		hash, err := bcrypt.GenerateFromPassword([]byte(s.adminPassword), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("authservice: bootstrap: hash password: %w", err)
		}

		now := time.Now().UTC()
		return s.repo.Create(ctx, &model.Auth{
			Username:     s.adminUsername,
			Password:     string(hash),
			TokenVersion: 1,
			CreatedAt:    now,
			UpdatedAt:    now,
		})
	}
	return nil
}
