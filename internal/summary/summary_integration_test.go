package summary_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caleflat/nfl/internal/api"
	"github.com/caleflat/nfl/internal/summary"
	"github.com/caleflat/nfl/internal/testutil"
)

func seededDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testutil.Postgres(t)
	_, err := db.Exec(`
INSERT INTO season_imports(season,source,source_sha256,source_obtained_at,contract_version,source_rows,stored_rows,eligible_rows,missing_distance_rows)
SELECT season, 'synthetic fixture', repeat('a',64), '2026-10-19T00:00:00Z'::timestamptz, 1, 100, 100, 0, 0
FROM (VALUES (2025),(2024)) AS seasons(season);

INSERT INTO plays(game_id,play_id,season,season_type,week,game_date,home_team,away_team,
    posteam,down,ydstogo,is_eligible,yards_gained,epa,air_yards,cpoe,pass_attempt,sack,
    receiver_player_id,third_down_converted,third_down_failed,fourth_down_converted,fourth_down_failed)
SELECT season || '_01_NE_BUF', id, season, 'REG', 1, DATE '2025-09-07', 'BUF', 'NE',
    team, down, distance, eligible, yards, epa, air, cpoe, pass_attempt, sack, receiver,
    converted, failed, converted4, failed4
FROM (VALUES
-- Six matching third-down plays. Targeted attempts are 1,2,3,6.
-- Sack/scramble air-yard and CPOE poison values must not enter passing metrics.
 (1,2025,'NE',3, 5,true, 10, 2.0, -2.0, 20.0,true,false,'r',true,false,false,false),
 (2,2025,'NE',3,10,true,  0,-1.0,  0.0,-20.0,true,false,'r',false,true,false,false),
 (3,2025,'NE',3, 5,true,NULL,0.0,12.0, NULL,true,false,'r',false,true,false,false),
 (4,2025,'NE',3,10,true, -5,-2.0, 99.0, 99.0,true,true,'r',false,false,false,false),
 (5,2025,'NE',3, 5,true,  8,NULL, 88.0, 88.0,false,false,NULL,true,false,false,false),
 (6,2025,'NE',3,10,true,  3,NULL, NULL, 10.0,true,false,'r',NULL,NULL,false,false),
-- Excluded population and filter distractors.
 (7,2025,'NE',3, 5,false,1000,1000.0,NULL,NULL,false,false,NULL,false,true,false,false),
 (8,2025,'NE',3, 4,true, 1000,1000.0,NULL,NULL,false,false,NULL,false,true,false,false),
 (9,2025,'NE',3,11,true, 1000,1000.0,NULL,NULL,false,false,NULL,false,true,false,false),
 (10,2025,'NE',3,NULL,true,1000,1000.0,NULL,NULL,false,false,NULL,false,true,false,false),
 (11,2025,'NE',2,5,true,1000,1000.0,NULL,NULL,false,false,NULL,false,true,false,false),
 (12,2025,'BUF',3,5,true,1000,1000.0,NULL,NULL,false,false,NULL,false,true,false,false),
 (13,2024,'NE',3,5,true,1000,1000.0,NULL,NULL,false,false,NULL,false,true,false,false),
-- Fourth-down conflict and a classified conversion; no targets in this group.
 (14,2025,'NE',4,5,true,0,0.0,NULL,NULL,false,false,NULL,false,false,true,true),
 (15,2025,'NE',4,5,true,2,1.0,NULL,NULL,false,false,NULL,false,false,true,false),
-- First-down sample with missing measurements is not an empty play sample.
 (16,2025,'NE',1,5,true,NULL,NULL,NULL,NULL,false,false,NULL,false,false,false,false)
) AS f(id,season,team,down,distance,eligible,yards,epa,air,cpoe,pass_attempt,sack,receiver,converted,failed,converted4,failed4);

UPDATE season_imports AS i SET source_rows=s.n,stored_rows=s.n,eligible_rows=s.eligible
FROM (SELECT season,count(*)::integer AS n,count(*) FILTER (WHERE is_eligible)::integer AS eligible FROM plays GROUP BY season) AS s
WHERE i.season=s.season;`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func closeTo(t *testing.T, value *float64, expected float64) {
	t.Helper()
	if value == nil || math.Abs(*value-expected) > 1e-10 {
		t.Fatalf("got %v; want %v", value, expected)
	}
}

func filters(down int) summary.Filters {
	return summary.Filters{Season: 2025, Team: "NE", Down: down, MinDistance: 5, MaxDistance: 10}
}

func TestSummaryHandCalculatedMetrics(t *testing.T) {
	db := seededDB(t)
	r, err := (summary.Store{DB: db}).Summary(context.Background(), filters(3))
	if err != nil {
		t.Fatal(err)
	}
	// Yards: (10+0-5+8+3)/5=3.2. EPA: (2-1+0-2)/4=-0.25;
	// only one EPA is positive. Missing EPA must not count as failure.
	if r.PlayCount != 6 || r.YardsPerPlay.Count != 5 || r.YardsPerPlay.Missing != 1 || r.EPA.Count != 4 || r.EPA.Missing != 2 {
		t.Fatalf("wrong base/metric counts: %+v", r)
	}
	closeTo(t, r.YardsPerPlay.Sum, 16)
	closeTo(t, r.YardsPerPlay.Value, 3.2)
	closeTo(t, r.EPA.Total, -1)
	closeTo(t, r.EPA.PerPlay, -0.25)
	closeTo(t, r.EPA.SuccessRate, 0.25)
	if r.EPA.SuccessfulPlays != 1 {
		t.Fatalf("zero EPA is not success: %+v", r.EPA)
	}
	// Targets: four. Air yards: (-2+0+12)/3; CPOE: (20-20+10)/3.
	if r.TargetedAttempts != 4 || r.ADOT.Count != 3 || r.ADOT.Missing != 1 || r.CPOE.Count != 3 || r.CPOE.Missing != 1 {
		t.Fatalf("wrong targeted population: %+v", r)
	}
	closeTo(t, r.ADOT.Sum, 10)
	closeTo(t, r.ADOT.Value, 10.0/3)
	closeTo(t, r.CPOE.Value, 10.0/3)
	if r.ConversionRate == nil || r.ConversionRate.Classified != 4 || r.ConversionRate.Converted != 2 || r.ConversionRate.Failed != 2 || r.ConversionRate.Unclassified != 2 {
		t.Fatalf("wrong conversion classification: %+v", r.ConversionRate)
	}
	closeTo(t, r.ConversionRate.Value, 0.5)
	if r.Dataset.SourceSHA256 != strings.Repeat("a", 64) || r.Dataset.ContractVersion != 1 || r.Dataset.PublishedAt.IsZero() {
		t.Fatalf("missing provenance: %+v", r.Dataset)
	}
}

func TestSummaryBoundariesEmptyAndMissing(t *testing.T) {
	db := seededDB(t)
	store := summary.Store{DB: db}
	f := filters(3)
	f.MaxDistance = 5
	r, err := store.Summary(context.Background(), f)
	if err != nil || r.PlayCount != 3 {
		t.Fatalf("inclusive distance equality: %+v %v", r, err)
	}
	closeTo(t, r.YardsPerPlay.Value, 9) // (10+8)/2, not an average of subgroup averages.
	f.MinDistance, f.MaxDistance = 6, 9
	r, err = store.Summary(context.Background(), f)
	if err != nil || r.PlayCount != 0 || r.EPA.Total != nil || r.EPA.PerPlay != nil || r.EPA.SuccessRate != nil || r.ADOT.Value != nil || r.ConversionRate.Value != nil {
		t.Fatalf("empty sample should have null metrics: %+v %v", r, err)
	}
	r, err = store.Summary(context.Background(), filters(1))
	if err != nil || r.PlayCount != 1 || r.YardsPerPlay.Missing != 1 || r.EPA.Missing != 1 || r.ConversionRate != nil {
		t.Fatalf("missing sample differs from empty/inapplicable: %+v %v", r, err)
	}
	r, err = store.Summary(context.Background(), filters(4))
	if err != nil || r.ConversionRate.Classified != 1 || r.ConversionRate.Unclassified != 1 || r.TargetedAttempts != 0 || r.ADOT.Missing != 0 || r.CPOE.Value != nil {
		t.Fatalf("fourth-down flags / empty target population: %+v %v", r, err)
	}
	closeTo(t, r.ConversionRate.Value, 1)
	closeTo(t, r.EPA.SuccessRate, 0.5)
	f = filters(3)
	f.Season = 2023
	if _, err := store.Summary(context.Background(), f); !errors.Is(err, summary.ErrSeasonUnavailable) {
		t.Fatalf("unavailable season: %v", err)
	}
	f = filters(3)
	f.Team = "XYZ"
	if _, err := store.Summary(context.Background(), f); !errors.Is(err, summary.ErrUnknownTeam) {
		t.Fatalf("unknown team: %v", err)
	}
}

func TestHTTPWithRealPostgresAndCancellation(t *testing.T) {
	db := seededDB(t)
	handler := api.NewHandler(summary.Store{DB: db}, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Second)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/summary?season=2025&team=NE&down=3&min_distance=5&max_distance=10", nil))
	var r summary.Result
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &r) != nil || r.PlayCount != 6 {
		t.Fatalf("end-to-end summary: %d %s", w.Code, w.Body.String())
	}
	closeTo(t, r.ADOT.Value, 10.0/3)
	// Block the real SELECT and prove the deadline reaches PostgreSQL, not just
	// a fake store. It must return 504 promptly and leave the pool usable.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`LOCK TABLE plays IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	handler = api.NewHandler(summary.Store{DB: db}, slog.New(slog.NewTextHandler(io.Discard, nil)), 100*time.Millisecond)
	w = httptest.NewRecorder()
	start := time.Now()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/summary?season=2025&team=NE&down=3", nil))
	if w.Code != 504 || time.Since(start) > 3*time.Second {
		t.Fatalf("deadline did not cancel blocked query: %d %s", w.Code, time.Since(start))
	}
	tx.Rollback()
	if _, err := (summary.Store{DB: db}).Summary(context.Background(), filters(3)); err != nil {
		t.Fatalf("pool unusable after query cancellation: %v", err)
	}
}
