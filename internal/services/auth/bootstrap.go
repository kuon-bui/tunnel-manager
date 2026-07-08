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
	existing, err := s.repo.GetByUsername(ctx, s.adminUsername)
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
			Username:  s.adminUsername,
			Password:  string(hash),
			CreatedAt: now,
			UpdatedAt: now,
		})
	}

	if bcrypt.CompareHashAndPassword([]byte(existing.Password), []byte(s.adminPassword)) == nil {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(s.adminPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("authservice: bootstrap: hash password: %w", err)
	}
	existing.Password = string(hash)
	existing.UpdatedAt = time.Now().UTC()

	if err := s.repo.Update(ctx, existing); err != nil {
		return fmt.Errorf("authservice: bootstrap: update user: %w", err)
	}
	return nil
}
