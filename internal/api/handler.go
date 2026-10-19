package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/caleflat/nfl/internal/summary"
)

type SummaryStore interface {
	Summary(context.Context, summary.Filters) (summary.Result, error)
}

func NewHandler(store SummaryStore, logger *slog.Logger, queryTimeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		status := serve(w, r, store, logger, queryTimeout)
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", status, "duration", time.Since(start))
	})
}

func serve(w http.ResponseWriter, r *http.Request, store SummaryStore, logger *slog.Logger, queryTimeout time.Duration) int {
	if r.URL.Path != "/api/v1/summary" && r.URL.Path != "/healthz" {
		return failure(w, http.StatusNotFound, "not_found", "route not found")
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		return failure(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
	}
	if r.URL.Path == "/healthz" {
		return respond(w, http.StatusOK, map[string]string{"status": "ok"})
	}
	filters, err := parseFilters(r.URL.RawQuery)
	if err != nil {
		return failure(w, http.StatusBadRequest, "invalid_filters", err.Error())
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	result, err := store.Summary(ctx, filters)
	if err == nil {
		return respond(w, http.StatusOK, result)
	}
	if r.Context().Err() != nil {
		// The client has gone away; cancellation has already reached the store.
		return 0
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return failure(w, http.StatusGatewayTimeout, "query_timeout", "summary query timed out")
	case errors.Is(err, summary.ErrUnknownTeam):
		return failure(w, http.StatusBadRequest, "unknown_team", err.Error())
	case errors.Is(err, summary.ErrSeasonUnavailable):
		return failure(w, http.StatusNotFound, "season_unavailable", err.Error())
	default:
		logger.Error("summary query failed", "error", err)
		return failure(w, http.StatusInternalServerError, "internal_error", "could not load summary")
	}
}

func parseFilters(raw string) (summary.Filters, error) {
	f := summary.Filters{MinDistance: 0, MaxDistance: 100}
	if len(raw) > 2048 {
		return f, errors.New("query string is too long")
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return f, errors.New("malformed query string")
	}
	for key, values := range q {
		switch key {
		case "season", "team", "down", "min_distance", "max_distance":
		default:
			return f, errors.New("unknown query parameter: " + key)
		}
		if len(values) != 1 || values[0] == "" {
			return f, errors.New("query parameters must have one nonempty value: " + key)
		}
	}
	f.Team = q.Get("team")
	for _, param := range []struct {
		name     string
		target   *int
		required bool
	}{
		{"season", &f.Season, true}, {"down", &f.Down, true},
		{"min_distance", &f.MinDistance, false}, {"max_distance", &f.MaxDistance, false},
	} {
		raw := q.Get(param.name)
		if raw == "" && !param.required {
			continue
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return f, errors.New(param.name + " must be an integer")
		}
		*param.target = value
	}
	return f, f.Validate()
}

func failure(w http.ResponseWriter, status int, code, message string) int {
	return respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func respond(w http.ResponseWriter, status int, value any) int {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
	return status
}
