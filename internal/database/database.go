package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Open checks connectivity without including credentials in returned errors.
func Open(ctx context.Context, url string) (*sql.DB, error) {
	if url == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, errors.New("cannot connect to database; check DATABASE_URL and PostgreSQL availability")
	}
	return db, nil
}
