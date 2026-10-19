package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caleflat/nfl/internal/plays"
)

type playStoreFunc func(context.Context, plays.Request) (plays.Result, error)

func (f playStoreFunc) Plays(ctx context.Context, req plays.Request) (plays.Result, error) {
	return f(ctx, req)
}

func TestInvalidPlayRequestsNeverReachDatabase(t *testing.T) {
	store := playStoreFunc(func(context.Context, plays.Request) (plays.Result, error) {
		t.Fatal("invalid pagination reached database")
		return plays.Result{}, nil
	})
	for _, suffix := range []string{
		"&limit=0", "&limit=101", "&limit=1.5", "&offset=-1", "&offset=100001", "&offset=bad",
		"&limit=2&limit=3", "&offset=", "&source_sha256=abc", "&source_sha256=" + strings.Repeat("A", 64),
		"&source_sha256=" + strings.Repeat("z", 64), "&unknown=1", "&min_distance=10&max_distance=5",
	} {
		t.Run(suffix, func(t *testing.T) {
			w := httptest.NewRecorder()
			NewHandler(nil, store, logger(), time.Second).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/plays?season=2025&team=NE&down=3"+suffix, nil))
			if w.Code != 400 || !strings.Contains(w.Body.String(), "invalid_filters") {
				t.Fatalf("invalid play parameters: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestPlayDefaultsAndDatasetConflict(t *testing.T) {
	store := playStoreFunc(func(ctx context.Context, req plays.Request) (plays.Result, error) {
		if req.Limit != 25 || req.Offset != 0 || req.Filters.MinDistance != 0 || req.Filters.MaxDistance != 100 {
			t.Fatalf("play defaults: %+v", req)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing query deadline")
		}
		return plays.Result{}, plays.ErrDatasetChanged
	})
	w := httptest.NewRecorder()
	NewHandler(nil, store, logger(), time.Second).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/plays?season=2025&team=NE&down=3", nil))
	if w.Code != 409 || !strings.Contains(w.Body.String(), "dataset_changed") {
		t.Fatalf("dataset guard: %d %s", w.Code, w.Body.String())
	}
}

func TestPlayErrorsAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name   string
		store  playStoreFunc
		status int
	}{
		{"timeout", func(ctx context.Context, _ plays.Request) (plays.Result, error) {
			<-ctx.Done()
			return plays.Result{}, ctx.Err()
		}, 504},
		{"private error", func(context.Context, plays.Request) (plays.Result, error) {
			return plays.Result{}, errors.New("private SQL details")
		}, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			NewHandler(nil, tc.store, logger(), 10*time.Millisecond).ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/plays?season=2025&team=NE&down=3", nil))
			if w.Code != tc.status || strings.Contains(w.Body.String(), "private SQL") {
				t.Fatalf("play error: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
