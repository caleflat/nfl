package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caleflat/nfl/internal/summary"
)

type storeFunc func(context.Context, summary.Filters) (summary.Result, error)

func (f storeFunc) Summary(ctx context.Context, filters summary.Filters) (summary.Result, error) {
	return f(ctx, filters)
}

func logger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestInvalidRequestsDoNotQueryDatabase(t *testing.T) {
	store := storeFunc(func(context.Context, summary.Filters) (summary.Result, error) {
		t.Fatal("invalid request reached store")
		return summary.Result{}, nil
	})
	base := "season=2025&team=NE&down=3"
	for _, query := range []string{
		"", "season=2025&team=NE", "season=2025&down=3", "season=x&team=NE&down=3",
		base + "&season=2024", base + "&team=", base + "&unexpected=1", base + "&min_distance=10&max_distance=5",
		base + "&min_distance=-1", base + "&max_distance=101", base + "&min_distance=1.5",
		"season=2025&team=ne&down=3", "season=2025&team=NE&down=5", "season=2101&team=NE&down=3",
		base + "&min_distance=%zz", base + ";broken", base + "&" + strings.Repeat("x", 2048),
	} {
		t.Run(query, func(t *testing.T) {
			w := httptest.NewRecorder()
			NewHandler(store, nil, logger(), time.Second).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/summary?"+query, nil))
			if w.Code != 400 || w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("invalid filters: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestSummaryDefaultsAndNullJSON(t *testing.T) {
	store := storeFunc(func(ctx context.Context, f summary.Filters) (summary.Result, error) {
		if f != (summary.Filters{Season: 2025, Team: "NE", Down: 1, MinDistance: 0, MaxDistance: 100}) {
			t.Fatalf("unexpected defaults: %+v", f)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("store did not receive a deadline")
		}
		return summary.Result{Filters: f}, nil
	})
	w := httptest.NewRecorder()
	NewHandler(store, nil, logger(), time.Second).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/summary?season=2025&team=NE&down=1", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("success response: %d %s", w.Code, w.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if string(body["conversion_rate"]) != "null" || !strings.Contains(string(body["adot"]), `"value":null`) {
		t.Fatalf("unavailable metrics must serialize as null: %s", w.Body.String())
	}
}

func TestErrorMappingAndRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		storeError         error
		status             int
		code               string
	}{
		{"missing season", "GET", "/api/v1/summary?season=2025&team=NE&down=3", summary.ErrSeasonUnavailable, 404, "season_unavailable"},
		{"unknown team", "GET", "/api/v1/summary?season=2025&team=XYZ&down=3", summary.ErrUnknownTeam, 400, "unknown_team"},
		{"database error", "GET", "/api/v1/summary?season=2025&team=NE&down=3", errors.New("private database details"), 500, "internal_error"},
		{"method", "POST", "/api/v1/summary", nil, 405, "method_not_allowed"},
		{"route", "GET", "/missing", nil, 404, "not_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := storeFunc(func(context.Context, summary.Filters) (summary.Result, error) { return summary.Result{}, tc.storeError })
			w := httptest.NewRecorder()
			NewHandler(store, nil, logger(), time.Second).ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) || strings.Contains(w.Body.String(), "private database") {
				t.Fatalf("error mapping: %d %s", w.Code, w.Body.String())
			}
			if tc.status == 405 && w.Header().Get("Allow") != "GET" {
				t.Fatal("405 missing Allow header")
			}
		})
	}
}

func TestDeadlineAndClientCancellationReachStore(t *testing.T) {
	store := storeFunc(func(ctx context.Context, _ summary.Filters) (summary.Result, error) {
		<-ctx.Done()
		return summary.Result{}, ctx.Err()
	})
	handler := NewHandler(store, nil, logger(), 10*time.Millisecond)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/summary?season=2025&team=NE&down=3", nil))
	if w.Code != 504 || !strings.Contains(w.Body.String(), "query_timeout") {
		t.Fatalf("deadline: %d %s", w.Code, w.Body.String())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/summary?season=2025&team=NE&down=3", nil).WithContext(ctx)
	handler.ServeHTTP(w, r)
	if w.Body.Len() != 0 {
		t.Fatal("wrote response for canceled client")
	}
}

func TestHealthDoesNotQueryDatabase(t *testing.T) {
	store := storeFunc(func(context.Context, summary.Filters) (summary.Result, error) {
		t.Fatal("health must not query database")
		return summary.Result{}, nil
	})
	w := httptest.NewRecorder()
	NewHandler(store, nil, logger(), time.Second).ServeHTTP(w, httptest.NewRequest("GET", "/healthz", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Fatalf("health: %d %s", w.Code, w.Body.String())
	}
}
