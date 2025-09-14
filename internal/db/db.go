package db

import (
	"context"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

func MustOpen() *sqlx.DB {
	dsn := os.Getenv("DATABASE_URL")
	db := sqlx.MustConnect("pgx", dsn)
	return db
}

func WithTx(ctx context.Context, db *sqlx.DB, fn func(*sqlx.Tx) error) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
