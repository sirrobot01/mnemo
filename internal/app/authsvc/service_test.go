package authsvc_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sirrobot01/mnemo/internal/app/authsvc"
	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/migrations"
	"github.com/sirrobot01/mnemo/internal/storage/sqlite"
)

func newStore(t *testing.T) *sqlite.Adapter {
	t.Helper()
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "mnemo.db")
	if _, err := migrations.ApplySQLite(ctx, dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	a, err := sqlite.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { a.Close() })
	return a
}

func TestSignupLoginAuthenticate(t *testing.T) {
	ctx := context.Background()
	svc := authsvc.New(newStore(t), time.Hour)

	if _, err := svc.Signup(ctx, "Dev@Example.com", "short"); err != authsvc.ErrWeakPassword {
		t.Fatalf("weak password not rejected: %v", err)
	}
	user, err := svc.Signup(ctx, "Dev@Example.com", "correct horse battery")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if user.Email != "dev@example.com" {
		t.Fatalf("email not normalized: %q", user.Email)
	}
	if _, err := svc.Signup(ctx, "dev@example.com", "another password"); err != authsvc.ErrEmailTaken {
		t.Fatalf("duplicate email not rejected: %v", err)
	}

	if _, err := svc.Login(ctx, "dev@example.com", "wrong"); err != authsvc.ErrInvalidCredentials {
		t.Fatalf("bad password should fail: %v", err)
	}
	tok, err := svc.Login(ctx, "dev@example.com", "correct horse battery")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	got, err := svc.Authenticate(ctx, tok.Token)
	if err != nil || got.ID != user.ID {
		t.Fatalf("authenticate: user=%+v err=%v", got, err)
	}

	if err := svc.Logout(ctx, tok.Token); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := svc.Authenticate(ctx, tok.Token); err == nil {
		t.Fatal("revoked token must not authenticate")
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	svc := authsvc.New(store, time.Hour)
	user, err := svc.Signup(ctx, "a@b.com", "password123")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	// Insert an already-expired token directly through the store.
	expired := domain.AuthToken{
		Token:     "expired-token",
		UserID:    user.ID,
		ExpiresAt: time.Now().Add(-time.Hour),
		CreatedAt: time.Now().Add(-2 * time.Hour),
	}
	if err := store.CreateToken(ctx, expired); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	if _, err := svc.Authenticate(ctx, "expired-token"); err != authsvc.ErrTokenExpired {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}
