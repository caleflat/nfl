package importer

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caleflat/nfl/internal/database"
	"github.com/caleflat/nfl/internal/pbp"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run real PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, `CREATE SCHEMA `+schema); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(ctx, `DROP SCHEMA `+schema+` CASCADE`); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := database.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatalf("repeated migration: %v", err)
	}
	return db
}

func fixture(t *testing.T, changes ...map[string]string) string {
	t.Helper()
	header := pbp.Columns()
	header = header[:len(header)-1] // is_eligible is derived, not a source field.
	for i, name := range header {
		if name == "description" {
			header[i] = "desc"
		}
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	w.Write(header)
	for _, change := range changes {
		values := map[string]string{
			"game_id": "2025_01_NE_BUF", "play_id": "1", "season": "2025", "season_type": "REG",
			"week": "1", "game_date": "2025-09-07", "home_team": "BUF", "away_team": "NE",
			"posteam": "NE", "defteam": "BUF", "down": "3", "ydstogo": "5", "qtr": "1",
			"play_type": "pass", "qb_kneel": "0", "qb_spike": "0", "two_point_attempt": "0",
			"play_deleted": "0", "aborted_play": "0", "pass_attempt": "1", "sack": "0",
			"yards_gained": "7", "epa": "0.5", "air_yards": "-2", "desc": "Synthetic test play",
		}
		for k, v := range change {
			values[k] = v
		}
		row := make([]string, len(header))
		for i, key := range header {
			row[i] = values[key]
		}
		w.Write(row)
	}
	w.Flush()
	return b.String()
}

func options(season int) Options {
	return Options{Season: season, Source: "synthetic test fixture", SourceObtained: time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)}
}

func mustImport(t *testing.T, db *sql.DB, data string, season int) Result {
	t.Helper()
	r, err := Import(context.Background(), db, strings.NewReader(data), options(season))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestImportReplacementAndSeasonIsolation(t *testing.T) {
	db := testDB(t)
	initial := fixture(t, map[string]string{}, map[string]string{"play_id": "2"}, map[string]string{"play_id": "3", "season_type": "POST"})
	r := mustImport(t, db, initial, 2025)
	if r.SourceRows != 3 || r.StoredRows != 2 || r.EligibleRows != 2 {
		t.Fatalf("wrong population: %+v", r)
	}
	mustImport(t, db, fixture(t, map[string]string{"game_id": "2024_01_NE_BUF", "season": "2024", "game_date": "2024-09-08"}), 2024)
	corrected := fixture(t, map[string]string{"yards_gained": "12", "air_yards": "", "epa": "0"})
	for range 2 {
		r = mustImport(t, db, corrected, 2025)
		var count, yards, other int
		var air *float64
		if err := db.QueryRow(`SELECT count(*),sum(yards_gained),max(air_yards) FROM plays WHERE season=2025`).Scan(&count, &yards, &air); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM plays WHERE season=2024`).Scan(&other); err != nil {
			t.Fatal(err)
		}
		if count != 1 || yards != 12 || air != nil || other != 1 {
			t.Fatalf("replacement/null/season isolation: %d %d %v %d", count, yards, air, other)
		}
	}
}

func TestFailedImportPreservesDataAndProvenance(t *testing.T) {
	db := testDB(t)
	original := mustImport(t, db, fixture(t, map[string]string{}), 2025)
	cases := map[string]string{
		"late duplicate":      fixture(t, map[string]string{"yards_gained": "20"}, map[string]string{}),
		"padded duplicate":    fixture(t, map[string]string{}, map[string]string{"play_id": "01"}),
		"late parse error":    fixture(t, map[string]string{"yards_gained": "20"}, map[string]string{"play_id": "2", "epa": "oops"}),
		"mixed season":        fixture(t, map[string]string{}, map[string]string{"play_id": "2", "season": "2024"}),
		"database constraint": fixture(t, map[string]string{"yards_gained": "20"}, map[string]string{"play_id": "2", "down": "5"}),
		"unknown team":        fixture(t, map[string]string{"posteam": "XYZ"}),
		"empty replacement":   fixture(t),
		"postseason only":     fixture(t, map[string]string{"season_type": "POST"}),
		"bad header":          "game_id\ninvalid\n",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Import(context.Background(), db, strings.NewReader(input), options(2025)); err == nil {
				t.Fatal("expected import failure")
			}
			var hash string
			var count, yards int
			if err := db.QueryRow(`SELECT source_sha256 FROM season_imports WHERE season=2025`).Scan(&hash); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*),sum(yards_gained) FROM plays WHERE season=2025`).Scan(&count, &yards); err != nil {
				t.Fatal(err)
			}
			if hash != original.SHA256 || count != 1 || yards != 7 {
				t.Fatalf("failed replacement changed committed state: %s %d %d", hash, count, yards)
			}
		})
	}
}

func TestImportLockCancellationAndReaderVisibility(t *testing.T) {
	db := testDB(t)
	mustImport(t, db, fixture(t, map[string]string{}), 2025)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock(71002, 2025)`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`DELETE FROM season_imports WHERE season=2025`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err = Import(ctx, db, strings.NewReader(fixture(t, map[string]string{"yards_gained": "99"})), options(2025))
	if err == nil || ctx.Err() == nil {
		t.Fatalf("same-season import should wait and cancel: %v", err)
	}
	var yards int
	if err := db.QueryRow(`SELECT yards_gained FROM plays WHERE season=2025`).Scan(&yards); err != nil || yards != 7 {
		t.Fatalf("reader lost prior publication: %d, %v", yards, err)
	}
	tx.Rollback()
	mustImport(t, db, fixture(t, map[string]string{"yards_gained": "12"}), 2025)
}

func TestMigrationsRejectEditedHistory(t *testing.T) {
	db := testDB(t)
	if _, err := db.Exec(`UPDATE schema_migrations SET sha256='modified'`); err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(context.Background(), db); err == nil {
		t.Fatal("expected checksum rejection")
	}
}
