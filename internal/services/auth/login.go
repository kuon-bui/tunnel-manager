package authservice

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/jwt"

	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("authservice: invalid credentials")

var dummyPasswordHash []byte
var letters = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ1234567890!@#$%^&*()-_=+[]{}|;:,.<>?/~`")

func init() {
	length := rand.Intn(16) + 16 // Random length between 16 and 32
	dummyPassword := make([]byte, length)

	for i := range dummyPassword {
		dummyPassword[i] = letters[rand.Intn(len(letters))]
	}

	var err error
	dummyPasswordHash, err = bcrypt.GenerateFromPassword(dummyPassword, bcrypt.DefaultCost)
	if err != nil {
		panic(fmt.Sprintf("authservice: failed to generate dummy password hash: %v", err))
	}
}

func (s *authService) Login(ctx context.Context, username, password string) (string, time.Time, error) {
	auth, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
			return "", time.Time{}, ErrInvalidCredentials
		}
		return "", time.Time{}, fmt.Errorf("authservice: get user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(auth.Password), []byte(password)); err != nil {
		return "", time.Time{}, ErrInvalidCredentials
	}

	token, expiresAt, err := jwt.GenerateToken(s.secret, auth.Username, s.ttl)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("authservice: generate token: %w", err)
	}

	return token, expiresAt, nil
}
