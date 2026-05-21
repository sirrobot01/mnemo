package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/sirrobot01/mnemo/internal/domain"
)

func (a *Adapter) CreateUser(ctx context.Context, user domain.User) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO users (id, email, password_hash, created_at) VALUES ($1,$2,$3,$4)`,
		user.ID, user.Email, user.PasswordHash, user.CreatedAt.UTC(),
	)
	return err
}

func (a *Adapter) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	db, err := a.conn()
	if err != nil {
		return domain.User{}, err
	}
	return scanUser(db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = $1`, email))
}

func (a *Adapter) GetUserByID(ctx context.Context, id domain.ID) (domain.User, error) {
	db, err := a.conn()
	if err != nil {
		return domain.User{}, err
	}
	return scanUser(db.QueryRowContext(ctx,
		`SELECT id, email, password_hash, created_at FROM users WHERE id = $1`, id))
}

func scanUser(s rowScanner) (domain.User, error) {
	var u domain.User
	if err := s.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.User{}, ErrNotFound
		}
		return domain.User{}, err
	}
	return u, nil
}

func (a *Adapter) CreateToken(ctx context.Context, token domain.AuthToken) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token, user_id, expires_at, created_at) VALUES ($1,$2,$3,$4)`,
		token.Token, token.UserID, token.ExpiresAt.UTC(), token.CreatedAt.UTC(),
	)
	return err
}

func (a *Adapter) GetToken(ctx context.Context, token string) (domain.AuthToken, error) {
	db, err := a.conn()
	if err != nil {
		return domain.AuthToken{}, err
	}
	var t domain.AuthToken
	err = db.QueryRowContext(ctx,
		`SELECT token, user_id, expires_at, created_at FROM auth_tokens WHERE token = $1`, token,
	).Scan(&t.Token, &t.UserID, &t.ExpiresAt, &t.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.AuthToken{}, ErrNotFound
		}
		return domain.AuthToken{}, err
	}
	return t, nil
}

func (a *Adapter) DeleteToken(ctx context.Context, token string) error {
	db, err := a.conn()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM auth_tokens WHERE token = $1`, token)
	return err
}
