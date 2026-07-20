package authservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"tunnelmanager/internal/model"
	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/jwt"

	"golang.org/x/crypto/bcrypt"
)

type fakeAuthRepo struct {
	users map[string]*model.Auth
}

func newFakeAuthRepo(users ...*model.Auth) *fakeAuthRepo {
	r := &fakeAuthRepo{users: make(map[string]*model.Auth)}
	for _, user := range users {
		copy := *user
		r.users[user.Username] = &copy
	}
	return r
}

func (r *fakeAuthRepo) GetByUsername(_ context.Context, username string) (*model.Auth, error) {
	user, ok := r.users[username]
	if !ok {
		return nil, model.ErrNotFound
	}
	copy := *user
	return &copy, nil
}

func (r *fakeAuthRepo) Create(_ context.Context, auth *model.Auth) error {
	if _, exists := r.users[auth.Username]; exists {
		return errors.New("duplicate")
	}
	copy := *auth
	r.users[auth.Username] = &copy
	return nil
}

func (r *fakeAuthRepo) Update(_ context.Context, auth *model.Auth) error {
	if _, exists := r.users[auth.Username]; !exists {
		return model.ErrNotFound
	}
	copy := *auth
	r.users[auth.Username] = &copy
	return nil
}

func TestChangePasswordRotatesTokenVersion(t *testing.T) {
	service, repo := testService(t, "old-password-123", 1)

	token, _, err := service.ChangePassword(context.Background(), "admin", "old-password-123", "new-password-456")
	if err != nil {
		t.Fatal(err)
	}
	updated := repo.users["admin"]
	if updated.TokenVersion != 2 {
		t.Fatalf("version = %d", updated.TokenVersion)
	}
	if bcrypt.CompareHashAndPassword([]byte(updated.Password), []byte("new-password-456")) != nil {
		t.Fatal("new password not stored")
	}
	username, version, err := jwt.ParseToken(testJWTSecret, token)
	if err != nil || username != "admin" || version != 2 {
		t.Fatalf("replacement token = %q/%d/%v", username, version, err)
	}
}

func TestChangePasswordRejectsWrongCurrentPassword(t *testing.T) {
	service, _ := testService(t, "old-password-123", 1)
	_, _, err := service.ChangePassword(context.Background(), "admin", "wrong-password", "new-password-456")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("err = %v", err)
	}
}

func TestChangePasswordRejectsPasswordByteLimits(t *testing.T) {
	for _, password := range []string{"short", strings.Repeat("a", 73)} {
		service, _ := testService(t, "old-password-123", 1)
		_, _, err := service.ChangePassword(context.Background(), "admin", "old-password-123", password)
		if !errors.Is(err, ErrInvalidPassword) {
			t.Fatalf("password bytes %d: err = %v", len([]byte(password)), err)
		}
	}

	service, _ := testService(t, "old-password-123", 1)
	if _, _, err := service.ChangePassword(context.Background(), "admin", "old-password-123", strings.Repeat("é", 6)); err != nil {
		t.Fatalf("12-byte multibyte password rejected: %v", err)
	}
}

func TestChangePasswordRejectsSamePassword(t *testing.T) {
	service, _ := testService(t, "same-password-123", 1)
	_, _, err := service.ChangePassword(context.Background(), "admin", "same-password-123", "same-password-123")
	if !errors.Is(err, ErrSamePassword) {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthenticateRejectsStaleVersion(t *testing.T) {
	service, _ := testService(t, "old-password-123", 2)
	token, _, err := jwt.GenerateToken(testJWTSecret, "admin", 1, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(context.Background(), token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v", err)
	}
}

func TestAuthenticateAcceptsCurrentVersion(t *testing.T) {
	service, _ := testService(t, "old-password-123", 2)
	token, _, err := jwt.GenerateToken(testJWTSecret, "admin", 2, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	username, err := service.Authenticate(context.Background(), token)
	if err != nil || username != "admin" {
		t.Fatalf("username/err = %q/%v", username, err)
	}
}

func TestLoginSignsCurrentVersion(t *testing.T) {
	service, _ := testService(t, "old-password-123", 4)
	token, _, err := service.Login(context.Background(), "admin", "old-password-123")
	if err != nil {
		t.Fatal(err)
	}
	_, version, err := jwt.ParseToken(testJWTSecret, token)
	if err != nil || version != 4 {
		t.Fatalf("version/err = %d/%v", version, err)
	}
}

func TestBootstrapCreatesMissingAdminOnce(t *testing.T) {
	repo := newFakeAuthRepo()
	service := NewAuthService(AuthServiceParams{Cfg: testConfig("initial-password-123"), Repo: repo})
	if err := service.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	created := repo.users["admin"]
	if created.TokenVersion != 1 {
		t.Fatalf("version = %d", created.TokenVersion)
	}
	if bcrypt.CompareHashAndPassword([]byte(created.Password), []byte("initial-password-123")) != nil {
		t.Fatal("seed password mismatch")
	}
}

func TestBootstrapPreservesExistingPasswordAndVersion(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("changed-password-123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeAuthRepo(&model.Auth{Username: "admin", Password: string(hash), TokenVersion: 7})
	service := NewAuthService(AuthServiceParams{Cfg: testConfig("env-password-123"), Repo: repo})
	if err := service.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	preserved := repo.users["admin"]
	if preserved.TokenVersion != 7 || bcrypt.CompareHashAndPassword([]byte(preserved.Password), []byte("changed-password-123")) != nil {
		t.Fatal("existing credentials changed")
	}
}

var testJWTSecret = []byte("01234567890123456789012345678901")

func testService(t *testing.T, password string, version int64) (AuthService, *fakeAuthRepo) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeAuthRepo(&model.Auth{Username: "admin", Password: string(hash), TokenVersion: version})
	return NewAuthService(AuthServiceParams{Cfg: testConfig(password), Repo: repo}), repo
}

func testConfig(adminPassword string) config.Config {
	return config.Config{
		AdminUsername: "admin",
		AdminPassword: adminPassword,
		JWTSecret:     testJWTSecret,
		JWTTTL:        time.Hour,
	}
}
