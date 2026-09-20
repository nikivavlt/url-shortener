package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db}
}

func (u UserRepository) Create(ctx context.Context, email string, passwordHash string) (string, error) {
	tx, err := u.db.Begin(ctx)
	if err != nil {

	}
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(
		ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`,
		email, passwordHash).
		Scan(&userID)
	if err != nil {
		return "", err
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO subscriptions (user_id, plan, links_limit) VALUES ($1, 'basic', 1000)`,
		userID,
	)
	if err != nil {
	}

	tx.Commit(ctx)

	return userID, nil
}
