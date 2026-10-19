package plays_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caleflat/nfl/internal/api"
	"github.com/caleflat/nfl/internal/plays"
	"github.com/caleflat/nfl/internal/summary"
	"github.com/caleflat/nfl/internal/testutil"
)

func seededDB(t *testing.T) *sql.DB {
	t.Helper()
	db := testutil.Postgres(t)
	_, err := db.Exec(`
INSERT INTO season_imports(season,source,source_sha256,source_obtained_at,contract_version,source_rows,stored_rows,eligible_rows,missing_distance_rows)
VALUES (2025,'synthetic fixture',repeat('a',64),now(),1,10,10,9,0);
INSERT INTO plays(game_id,play_id,season,season_type,week,game_date,home_team,away_team,
    posteam,down,ydstogo,is_eligible,description,yards_gained,epa,air_yards,cpoe,
    pass_attempt,sack,receiver_player_id,third_down_converted,third_down_failed)
SELECT game_id,id,2025,'REG',1,DATE '2025-09-07','BUF','NE',team,down,distance,eligible,
    'Synthetic play ' || id,yards,epa,air,cpoe,pass,sack,receiver,converted,failed
FROM (VALUES
-- Intentionally unsorted insertion order; identifiers sort numerically within games.
('2025_02_NE_BUF',2,'NE',3,5,true,4,1.0,8.0,20.0,true,false,'r',true,false),
('2025_01_NE_BUF',99,'NE',3,10,true,0,0.0,NULL,NULL,false,false,NULL,false,false),
('2025_01_NE_BUF',2,'NE',3,5,true,NULL,NULL,-2.0,NULL,true,false,'r',false,true),
('2025_02_NE_BUF',1,'NE',3,10,true,10,2.0,10.0,-10.0,true,false,'r',true,false),
('2025_01_NE_BUF',10,'NE',3,5,true,-5,-1.0,99.0,99.0,true,true,'r',true,true),
-- Records outside the common filters/population must not leak into pages.
('2025_01_NE_BUF',101,'NE',3,5,false,1000,10.0,NULL,NULL,false,false,NULL,false,true),
('2025_01_NE_BUF',102,'NE',3,4,true,1000,10.0,NULL,NULL,false,false,NULL,false,true),
('2025_01_NE_BUF',103,'NE',3,11,true,1000,10.0,NULL,NULL,false,false,NULL,false,true),
('2025_01_NE_BUF',104,'BUF',3,5,true,1000,10.0,NULL,NULL,false,false,NULL,false,true),
('2025_01_NE_BUF',105,'NE',2,5,true,1000,10.0,NULL,NULL,false,false,NULL,false,true)
) AS f(game_id,id,team,down,distance,eligible,yards,epa,air,cpoe,pass,sack,receiver,converted,failed);`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func request() plays.Request {
	return plays.Request{Filters: summary.Filters{Season: 2025, Team: "NE", Down: 3, MinDistance: 5, MaxDistance: 10}, Limit: 2}
}

func TestPagesAgreeWithSummaryAndHaveNoGaps(t *testing.T) {
	db := seededDB(t)
	store := plays.Store{DB: db}
	req := request()
	s, err := (summary.Store{DB: db}).Summary(context.Background(), req.Filters)
	if err != nil {
		t.Fatal(err)
	}
	req.SourceSHA256 = s.Dataset.SourceSHA256
	var keys []string
	var records []plays.Play
	for {
		r, err := store.Plays(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if r.Pagination.Total != s.PlayCount || r.Dataset.SourceSHA256 != s.Dataset.SourceSHA256 || len(r.Plays) > 2 {
			t.Fatalf("list and summary disagree: %+v", r)
		}
		for _, p := range r.Plays {
			keys = append(keys, p.GameID+"/"+strconv.Itoa(p.PlayID))
			records = append(records, p)
		}
		if r.Pagination.NextOffset == nil {
			break
		}
		if *r.Pagination.NextOffset <= req.Offset {
			t.Fatal("pagination did not advance")
		}
		req.Offset = *r.Pagination.NextOffset
	}
	want := []string{"2025_01_NE_BUF/2", "2025_01_NE_BUF/10", "2025_01_NE_BUF/99", "2025_02_NE_BUF/1", "2025_02_NE_BUF/2"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("missing, duplicated, or misordered records: %v", keys)
	}
	if records[0].EPA != nil || records[0].YardsGained != nil || records[0].AirYards == nil || *records[0].AirYards != -2 || !records[0].IdentifiedTarget {
		t.Fatalf("null/negative values lost: %+v", records[0])
	}
	if records[1].IdentifiedTarget || records[1].ConversionStatus != "unclassified" || records[2].EPA == nil || *records[2].EPA != 0 {
		t.Fatalf("sack, conflicting conversions, or zero EPA changed: %+v", records)
	}
	if records[0].GameDate != "2025-09-07" || records[0].Description == nil || records[0].Quarter != nil {
		t.Fatalf("context/nulls lost: %+v", records[0])
	}
	// An identical request returns the identical page, including an end offset.
	a, err := store.Plays(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Plays(context.Background(), req)
	if err != nil || !reflect.DeepEqual(a, b) {
		t.Fatalf("page not deterministic: %+v %v", b, err)
	}
}

func TestEmptyBeyondEndAndDatasetChanges(t *testing.T) {
	db := seededDB(t)
	store := plays.Store{DB: db}
	req := request()
	req.Offset = 100
	r, err := store.Plays(context.Background(), req)
	if err != nil || r.Pagination.Total != 5 || len(r.Plays) != 0 || r.Plays == nil || r.Pagination.NextOffset != nil {
		t.Fatalf("beyond end: %+v %v", r, err)
	}
	req.Offset, req.Filters.MinDistance, req.Filters.MaxDistance = 0, 6, 9
	r, err = store.Plays(context.Background(), req)
	if err != nil || r.Pagination.Total != 0 || len(r.Plays) != 0 || r.Plays == nil {
		t.Fatalf("empty sample: %+v %v", r, err)
	}
	req = request()
	req.SourceSHA256 = strings.Repeat("a", 64)
	if _, err := db.Exec(`UPDATE season_imports SET source_sha256=repeat('b',64)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Plays(context.Background(), req); !errors.Is(err, plays.ErrDatasetChanged) {
		t.Fatalf("stale hash must reject page: %v", err)
	}
	req.SourceSHA256 = ""
	r, err = store.Plays(context.Background(), req)
	if err != nil || r.Dataset.SourceSHA256 != strings.Repeat("b", 64) {
		t.Fatalf("fresh page: %+v %v", r, err)
	}
	req.Filters.Season = 2024
	if _, err := store.Plays(context.Background(), req); !errors.Is(err, summary.ErrSeasonUnavailable) {
		t.Fatalf("unavailable season: %v", err)
	}
	req.Filters.Team = "XYZ"
	if _, err := store.Plays(context.Background(), req); !errors.Is(err, summary.ErrUnknownTeam) {
		t.Fatalf("unknown team: %v", err)
	}
}

func TestPlayHTTPAndSQLCancellation(t *testing.T) {
	db := seededDB(t)
	h := api.NewHandler(summary.Store{DB: db}, plays.Store{DB: db}, slog.New(slog.NewTextHandler(io.Discard, nil)), 100*time.Millisecond)
	path := "/api/v1/plays?season=2025&team=NE&down=3&min_distance=5&max_distance=10&limit=2"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	var r plays.Result
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &r) != nil || len(r.Plays) != 2 || r.Pagination.Total != 5 {
		t.Fatalf("play HTTP result: %d %s", w.Code, w.Body.String())
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`LOCK TABLE plays IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != 504 {
		t.Fatalf("blocked list query did not time out: %d %s", w.Code, w.Body.String())
	}
	tx.Rollback()
	if _, err := (plays.Store{DB: db}).Plays(context.Background(), request()); err != nil {
		t.Fatalf("pool unusable after cancellation: %v", err)
	}
}

func TestConversionStatusUsesSelectedDown(t *testing.T) {
	db := seededDB(t)
	if _, err := db.Exec(`UPDATE plays SET down=4, fourth_down_converted=true, fourth_down_failed=false WHERE is_eligible`); err != nil {
		t.Fatal(err)
	}
	req := request()
	req.Filters.Down = 4
	r, err := (plays.Store{DB: db}).Plays(context.Background(), req)
	if err != nil || len(r.Plays) == 0 {
		t.Fatalf("fourth-down sample: %+v %v", r, err)
	}
	for _, p := range r.Plays {
		if p.ConversionStatus != "converted" {
			t.Fatalf("used third-down flags on fourth down: %+v", p)
		}
	}
	req.Filters.Down = 2
	if _, err := db.Exec(`UPDATE plays SET down=2 WHERE is_eligible`); err != nil {
		t.Fatal(err)
	}
	r, err = (plays.Store{DB: db}).Plays(context.Background(), req)
	if err != nil || len(r.Plays) == 0 || r.Plays[0].ConversionStatus != "not_applicable" {
		t.Fatalf("second-down conversions are unavailable: %+v %v", r, err)
	}
}
