package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nikivavlt/url-shortener/user/internal/service"
)

type UserRepository struct {
	db *pgxpool.Pool
}

func New(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

const uniqueViolation = "23505"

func (r *UserRepository) Create(ctx context.Context, email, passwordHash string) (string, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`,
		email, passwordHash,
	).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return "", service.ErrEmailExists
		}
		return "", fmt.Errorf("insert user: %w", err)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO subscriptions (user_id, plan, links_limit) VALUES ($1, 'basic', 1000)`,
		userID,
	)
	if err != nil {
		return "", fmt.Errorf("insert subscription: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit tx: %w", err)
	}
	return userID, nil
}

func (r *UserRepository) FindByEmail(ctx context.Context, email string) (userID, passwordHash string, err error) {
	err = r.db.QueryRow(ctx,
		`SELECT id, password_hash FROM users WHERE email = $1`,
		email,
	).Scan(&userID, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", service.ErrUserNotFound
	}
	if err != nil {
		return "", "", fmt.Errorf("find user by email: %w", err)
	}
	return userID, passwordHash, nil
}

func (r *UserRepository) CreateSession(ctx context.Context, userID string, expiresAt time.Time) (string, error) {
	var sid string
	err := r.db.QueryRow(ctx,
		`INSERT INTO sessions (user_id, expires_at) VALUES ($1, $2) RETURNING id`,
		userID, expiresAt,
	).Scan(&sid)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return sid, nil
}

func (r *UserRepository) GetActiveSessionUserID(ctx context.Context, sid string) (string, error) {
	var userID string
	err := r.db.QueryRow(ctx,
		`SELECT user_id FROM sessions
		 WHERE id = $1 AND is_revoked = false AND expires_at > now()`,
		sid,
	).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", service.ErrUnauthenticated
	}
	if err != nil {
		return "", fmt.Errorf("get active session: %w", err)
	}
	return userID, nil
}

func (r *UserRepository) RevokeSession(ctx context.Context, sid string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE sessions SET is_revoked = true, updated_at = now() WHERE id = $1`,
		sid,
	)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (r *UserRepository) GetSubscriptionLimit(ctx context.Context, userID string) (int32, error) {
	var limit int32
	err := r.db.QueryRow(ctx,
		`SELECT links_limit FROM subscriptions WHERE user_id = $1`,
		userID,
	).Scan(&limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, service.ErrUserNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("get subscription limit: %w", err)
	}
	return limit, nil
}
