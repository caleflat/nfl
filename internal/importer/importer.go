// Package importer publishes a regular season from a local nflverse CSV.
package importer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/caleflat/nfl/internal/pbp"
)

type Options struct {
	Season         int
	Source         string
	SourceObtained time.Time
}

type Result struct {
	Season              int    `json:"season"`
	SHA256              string `json:"sha256"`
	SourceRows          int    `json:"source_rows"`
	StoredRows          int    `json:"stored_rows"`
	EligibleRows        int    `json:"eligible_rows"`
	MissingDistanceRows int    `json:"missing_distance_rows"`
}

// Import replaces exactly one season in one transaction. A transaction-scoped
// advisory lock serializes same-season imports; cancellation releases the lock.
// Ordinary readers keep seeing the last committed season until publication.
func Import(ctx context.Context, db *sql.DB, input io.Reader, opts Options) (Result, error) {
	result := Result{Season: opts.Season}
	if opts.Season < 1999 || opts.Season > 2100 || strings.TrimSpace(opts.Source) == "" || opts.SourceObtained.IsZero() {
		return result, errors.New("season (1999–2100), source, and source timestamp are required")
	}
	hash := sha256.New()
	decoder, err := pbp.NewDecoder(io.TeeReader(input, hash))
	if err != nil {
		return result, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(71002, $1)`, opts.Season); err != nil {
		return result, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM season_imports WHERE season=$1`, opts.Season); err != nil {
		return result, err
	}
	// Provisional metadata and plays remain invisible outside this transaction.
	if _, err := tx.ExecContext(ctx, `INSERT INTO season_imports
		(season,source,source_sha256,source_obtained_at,contract_version,source_rows,stored_rows,eligible_rows,missing_distance_rows)
		VALUES ($1,$2,$3,$4,1,1,1,0,0)`, opts.Season, opts.Source, strings.Repeat("0", 64), opts.SourceObtained); err != nil {
		return result, err
	}
	columns := pbp.Columns()
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = "$" + strconv.Itoa(i+1)
	}
	statement, err := tx.PrepareContext(ctx, `INSERT INTO plays (`+strings.Join(columns, ",")+`) VALUES (`+strings.Join(placeholders, ",")+`)`)
	if err != nil {
		return result, err
	}
	defer statement.Close()
	seen := map[[2]string]bool{}
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		play, err := decoder.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, err
		}
		result.SourceRows++
		if play.Season != opts.Season {
			return result, fmt.Errorf("record %d: expected season %d, got %d", result.SourceRows, opts.Season, play.Season)
		}
		key := [2]string{play.GameID, play.PlayID}
		if seen[key] {
			return result, fmt.Errorf("record %d: duplicate game/play %s/%s", result.SourceRows, play.GameID, play.PlayID)
		}
		seen[key] = true
		if play.SeasonType != "REG" {
			continue
		}
		if _, err := statement.ExecContext(ctx, play.Values...); err != nil {
			return result, fmt.Errorf("record %d (%s/%s): %w", result.SourceRows, play.GameID, play.PlayID, err)
		}
		result.StoredRows++
		if play.Eligible {
			result.EligibleRows++
			if play.MissingDistance {
				result.MissingDistanceRows++
			}
		}
	}
	if result.StoredRows == 0 {
		return result, errors.New("source contains no regular-season records; refusing empty replacement")
	}
	result.SHA256 = hex.EncodeToString(hash.Sum(nil))
	if _, err := tx.ExecContext(ctx, `UPDATE season_imports SET source_sha256=$2,source_rows=$3,
		stored_rows=$4,eligible_rows=$5,missing_distance_rows=$6,published_at=clock_timestamp() WHERE season=$1`,
		opts.Season, result.SHA256, result.SourceRows, result.StoredRows, result.EligibleRows, result.MissingDistanceRows); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
