package authservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/jwt"

	"golang.org/x/crypto/bcrypt"
)

func (s *authService) Authenticate(ctx context.Context, token string) (string, error) {
	username, tokenVersion, err := jwt.ParseToken(s.secret, token)
	if err != nil {
		return "", ErrUnauthorized
	}
	auth, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return "", ErrUnauthorized
		}
		return "", fmt.Errorf("authservice: authenticate: get user: %w", err)
	}
	if auth.TokenVersion != tokenVersion {
		return "", ErrUnauthorized
	}
	return username, nil
}

func (s *authService) ChangePassword(ctx context.Context, username, currentPassword, newPassword string) (string, time.Time, error) {
	auth, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return "", time.Time{}, ErrInvalidCredentials
		}
		return "", time.Time{}, fmt.Errorf("authservice: change password: get user: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(auth.Password), []byte(currentPassword)) != nil {
		return "", time.Time{}, ErrInvalidCredentials
	}
	if passwordLength := len([]byte(newPassword)); passwordLength < 12 || passwordLength > 72 {
		return "", time.Time{}, ErrInvalidPassword
	}
	if bcrypt.CompareHashAndPassword([]byte(auth.Password), []byte(newPassword)) == nil {
		return "", time.Time{}, ErrSamePassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("authservice: change password: hash password: %w", err)
	}
	auth.Password = string(hash)
	auth.TokenVersion++
	auth.UpdatedAt = time.Now().UTC()
	if err := s.repo.Update(ctx, auth); err != nil {
		return "", time.Time{}, fmt.Errorf("authservice: change password: update user: %w", err)
	}

	token, expiresAt, err := jwt.GenerateToken(s.secret, auth.Username, auth.TokenVersion, s.ttl)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("authservice: change password: generate token: %w", err)
	}
	return token, expiresAt, nil
}
