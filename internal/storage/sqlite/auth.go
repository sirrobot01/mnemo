package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/sirrobot01/mnemo/internal/domain"
)

func (a *Adapter) CreateUser(ctx context.Context, user domain.User) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES (?, ?, ?, ?)`,
		user.ID, user.Email, user.PasswordHash, formatTime(user.CreatedAt),
	)
	return err
}

func (a *Adapter) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	return a.scanUser(a.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = ?`, email))
}

func (a *Adapter) GetUserByID(ctx context.Context, id domain.ID) (domain.User, error) {
	return a.scanUser(a.db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id = ?`, id))
}

func (a *Adapter) scanUser(row rowScanner) (domain.User, error) {
	var u domain.User
	var created string
	if err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, ErrNotFound
		}
		return domain.User{}, err
	}
	if t, err := parseTime(created); err == nil {
		u.CreatedAt = t
	}
	return u, nil
}

func (a *Adapter) CreateToken(ctx context.Context, token domain.AuthToken) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token, user_id, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		token.Token, token.UserID, formatTime(token.ExpiresAt), formatTime(token.CreatedAt),
	)
	return err
}

func (a *Adapter) GetToken(ctx context.Context, token string) (domain.AuthToken, error) {
	var t domain.AuthToken
	var expires, created string
	err := a.db.QueryRowContext(ctx,
		`SELECT token, user_id, expires_at, created_at FROM auth_tokens WHERE token = ?`, token,
	).Scan(&t.Token, &t.UserID, &expires, &created)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.AuthToken{}, ErrNotFound
		}
		return domain.AuthToken{}, err
	}
	t.ExpiresAt, _ = parseTime(expires)
	t.CreatedAt, _ = parseTime(created)
	return t, nil
}

func (a *Adapter) DeleteToken(ctx context.Context, token string) error {
	if a.db == nil {
		return fmt.Errorf("sqlite adapter is not open")
	}
	_, err := a.db.ExecContext(ctx, `DELETE FROM auth_tokens WHERE token = ?`, token)
	return err
}
