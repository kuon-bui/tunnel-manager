package jwt

import (
	"errors"
	"fmt"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	TokenVersion int64 `json:"tokenVersion"`
	jwtlib.RegisteredClaims
}

func GenerateToken(secret []byte, username string, tokenVersion int64, ttl time.Duration) (string, time.Time, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)

	claims := Claims{
		TokenVersion: tokenVersion,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   username,
			IssuedAt:  jwtlib.NewNumericDate(now),
			ExpiresAt: jwtlib.NewNumericDate(expiresAt),
		},
	}

	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("jwt: sign token: %w", err)
	}

	return signed, expiresAt, nil
}

func ParseToken(secret []byte, tokenString string) (string, int64, error) {
	claims := &Claims{}
	token, err := jwtlib.ParseWithClaims(tokenString, claims, func(t *jwtlib.Token) (any, error) {
		if _, ok := t.Method.(*jwtlib.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("jwt: unexpected signing method: %v", t.Header["alg"])
		}
		return secret, nil
	}, jwtlib.WithValidMethods([]string{jwtlib.SigningMethodHS256.Alg()}), jwtlib.WithExpirationRequired())
	if err != nil {
		return "", 0, fmt.Errorf("jwt: parse token: %w", err)
	}
	if !token.Valid {
		return "", 0, errors.New("jwt: invalid token")
	}
	if claims.Subject == "" || claims.TokenVersion < 1 {
		return "", 0, errors.New("jwt: invalid claims")
	}

	return claims.Subject, claims.TokenVersion, nil
}
