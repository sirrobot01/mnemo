// Package authsvc implements signup/login/authenticate for the Mnemo web/API
// surface. The ordinary CLI workflow does not use accounts; `mnemo serve`
// constructs this service when browser/API auth is enabled.
//
// Passwords are hashed with PBKDF2-HMAC-SHA256 (stdlib crypto/pbkdf2) — no
// external crypto dependency. Tokens are opaque 256-bit random strings.
package authsvc

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sirrobot01/mnemo/internal/domain"
	"github.com/sirrobot01/mnemo/internal/storage"
)

const (
	pbkdf2Iter    = 210000
	pbkdf2KeyLen  = 32
	saltLen       = 16
	tokenBytes    = 32
	defaultTTL    = 720 * time.Hour // 30 days
	minPassLength = 8
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrEmailTaken         = errors.New("email already registered")
	ErrWeakPassword       = fmt.Errorf("password must be at least %d characters", minPassLength)
	ErrTokenExpired       = errors.New("token expired")
)

// Service authenticates against the configured AuthStore.
type Service struct {
	store storage.AuthStore
	ttl   time.Duration
}

func New(store storage.AuthStore, ttl time.Duration) *Service {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	return &Service{store: store, ttl: ttl}
}

// Signup creates a new account. Email is normalized; the password is checked
// for minimum length and stored only as a PBKDF2 hash.
func (s *Service) Signup(ctx context.Context, email, password string) (domain.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return domain.User{}, ErrInvalidCredentials
	}
	if len(password) < minPassLength {
		return domain.User{}, ErrWeakPassword
	}
	if _, err := s.store.GetUserByEmail(ctx, email); err == nil {
		return domain.User{}, ErrEmailTaken
	} else if !errors.Is(err, storage.ErrNotFound) {
		return domain.User{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return domain.User{}, err
	}
	user := domain.User{
		ID:           domain.DeterministicID(domain.PrefixUser, email),
		Email:        email,
		PasswordHash: hash,
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.store.CreateUser(ctx, user); err != nil {
		return domain.User{}, err
	}
	return user, nil
}

// Login verifies credentials and issues a bearer token.
func (s *Service) Login(ctx context.Context, email, password string) (domain.AuthToken, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return domain.AuthToken{}, ErrInvalidCredentials
		}
		return domain.AuthToken{}, err
	}
	if !verifyPassword(password, user.PasswordHash) {
		return domain.AuthToken{}, ErrInvalidCredentials
	}
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return domain.AuthToken{}, err
	}
	now := time.Now().UTC()
	token := domain.AuthToken{
		Token:     hex.EncodeToString(raw),
		UserID:    user.ID,
		ExpiresAt: now.Add(s.ttl),
		CreatedAt: now,
	}
	if err := s.store.CreateToken(ctx, token); err != nil {
		return domain.AuthToken{}, err
	}
	return token, nil
}

// Authenticate resolves a bearer token to its user, rejecting expired tokens.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	tok, err := s.store.GetToken(ctx, strings.TrimSpace(token))
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return domain.User{}, ErrInvalidCredentials
		}
		return domain.User{}, err
	}
	if time.Now().UTC().After(tok.ExpiresAt) {
		_ = s.store.DeleteToken(ctx, tok.Token)
		return domain.User{}, ErrTokenExpired
	}
	return s.store.GetUserByID(ctx, tok.UserID)
}

// Logout revokes a token.
func (s *Service) Logout(ctx context.Context, token string) error {
	return s.store.DeleteToken(ctx, strings.TrimSpace(token))
}

// hashPassword returns "pbkdf2$<iter>$<salt b64>$<key b64>".
func hashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iter, pbkdf2KeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2$%d$%s$%s", pbkdf2Iter,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}
