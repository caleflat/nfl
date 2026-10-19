package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate applies ordered, forward-only SQL files atomically. Applied files are
// immutable: checksums detect edits. Rollbacks require a new forward migration.
func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(71001, 0)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name text PRIMARY KEY, sha256 text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		content, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		digest := sha256.Sum256(content)
		checksum := hex.EncodeToString(digest[:])
		var previous string
		err = tx.QueryRowContext(ctx, `SELECT sha256 FROM schema_migrations WHERE name=$1`, entry.Name()).Scan(&previous)
		switch {
		case err == nil:
			if previous != checksum {
				return fmt.Errorf("migration %s changed after application", entry.Name())
			}
			continue
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(name,sha256) VALUES ($1,$2)`, entry.Name(), checksum); err != nil {
			return err
		}
	}
	return tx.Commit()
}
