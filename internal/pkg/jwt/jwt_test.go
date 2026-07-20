package jwt

import (
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

var testSecret = []byte("01234567890123456789012345678901")

func TestTokenRoundTrip(t *testing.T) {
	token, expiresAt, err := GenerateToken(testSecret, "admin", 3, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	username, version, err := ParseToken(testSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	if username != "admin" || version != 3 {
		t.Fatalf("got %q/%d", username, version)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatal("expiry not in future")
	}
}

func TestParseTokenRejectsWrongSecret(t *testing.T) {
	token, _, err := GenerateToken(testSecret, "admin", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseToken([]byte("abcdefghijklmnopqrstuvwxyz123456"), token); err == nil {
		t.Fatal("expected wrong secret rejection")
	}
}

func TestParseTokenRejectsExpiredToken(t *testing.T) {
	token, _, err := GenerateToken(testSecret, "admin", 1, -time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseToken(testSecret, token); err == nil {
		t.Fatal("expected expired token rejection")
	}
}

func TestParseTokenRejectsInvalidClaims(t *testing.T) {
	for _, tc := range []struct {
		name     string
		username string
		version  int64
	}{
		{name: "empty subject", version: 1},
		{name: "zero version", username: "admin"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := signClaims(t, tc.username, tc.version, jwtlib.SigningMethodHS256)
			if _, _, err := ParseToken(testSecret, token); err == nil {
				t.Fatal("expected invalid claims rejection")
			}
		})
	}
}

func TestParseTokenRejectsUnexpectedAlgorithm(t *testing.T) {
	claims := Claims{
		TokenVersion: 1,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   "admin",
			IssuedAt:  jwtlib.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwtlib.NewNumericDate(time.Now().UTC().Add(time.Hour)),
		},
	}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodNone, claims)
	signed, err := token.SignedString(jwtlib.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseToken(testSecret, signed); err == nil {
		t.Fatal("expected algorithm rejection")
	}
}

func TestParseTokenRequiresExpiry(t *testing.T) {
	claims := Claims{
		TokenVersion: 1,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:  "admin",
			IssuedAt: jwtlib.NewNumericDate(time.Now().UTC()),
		},
	}
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, claims)
	signed, err := token.SignedString(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ParseToken(testSecret, signed); err == nil {
		t.Fatal("expected missing expiry rejection")
	}
}

func TestParseTokenRequiresHS256(t *testing.T) {
	token := signClaims(t, "admin", 1, jwtlib.SigningMethodHS512)
	if _, _, err := ParseToken(testSecret, token); err == nil {
		t.Fatal("expected non-HS256 rejection")
	}
}

func signClaims(t *testing.T, username string, version int64, method jwtlib.SigningMethod) string {
	t.Helper()
	claims := Claims{
		TokenVersion: version,
		RegisteredClaims: jwtlib.RegisteredClaims{
			Subject:   username,
			IssuedAt:  jwtlib.NewNumericDate(time.Now().UTC()),
			ExpiresAt: jwtlib.NewNumericDate(time.Now().UTC().Add(time.Hour)),
		},
	}
	token := jwtlib.NewWithClaims(method, claims)
	signed, err := token.SignedString(testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}
