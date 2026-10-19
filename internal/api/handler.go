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

	"github.com/caleflat/nfl/internal/plays"
	"github.com/caleflat/nfl/internal/summary"
)

type SummaryStore interface {
	Summary(context.Context, summary.Filters) (summary.Result, error)
}

type PlayStore interface {
	Plays(context.Context, plays.Request) (plays.Result, error)
}

func NewHandler(store SummaryStore, matches PlayStore, logger *slog.Logger, queryTimeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		status := serve(w, r, store, matches, logger, queryTimeout)
		logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", status, "duration", time.Since(start))
	})
}

func serve(w http.ResponseWriter, r *http.Request, store SummaryStore, matches PlayStore, logger *slog.Logger, queryTimeout time.Duration) int {
	if r.URL.Path != "/api/v1/summary" && r.URL.Path != "/api/v1/plays" && r.URL.Path != "/healthz" {
		return failure(w, http.StatusNotFound, "not_found", "route not found")
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		return failure(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
	}
	if r.URL.Path == "/healthz" {
		return respond(w, http.StatusOK, map[string]string{"status": "ok"})
	}
	ctx, cancel := context.WithTimeout(r.Context(), queryTimeout)
	defer cancel()
	var result any
	var err error
	if r.URL.Path == "/api/v1/plays" {
		var req plays.Request
		req, err = parsePlayRequest(r.URL.RawQuery)
		if err == nil {
			result, err = matches.Plays(ctx, req)
		} else {
			return failure(w, http.StatusBadRequest, "invalid_filters", err.Error())
		}
	} else {
		var filters summary.Filters
		filters, err = parseFilters(r.URL.RawQuery)
		if err == nil {
			result, err = store.Summary(ctx, filters)
		} else {
			return failure(w, http.StatusBadRequest, "invalid_filters", err.Error())
		}
	}
	if err == nil {
		return respond(w, http.StatusOK, result)
	}
	if r.Context().Err() != nil {
		// The client has gone away; cancellation has already reached the store.
		return 0
	}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return failure(w, http.StatusGatewayTimeout, "query_timeout", "query timed out")
	case errors.Is(err, plays.ErrDatasetChanged):
		return failure(w, http.StatusConflict, "dataset_changed", err.Error())
	case errors.Is(err, summary.ErrUnknownTeam):
		return failure(w, http.StatusBadRequest, "unknown_team", err.Error())
	case errors.Is(err, summary.ErrSeasonUnavailable):
		return failure(w, http.StatusNotFound, "season_unavailable", err.Error())
	default:
		logger.Error("query failed", "error", err)
		return failure(w, http.StatusInternalServerError, "internal_error", "could not load results")
	}
}

func parseFilters(raw string) (summary.Filters, error) {
	q, err := parseQuery(raw)
	if err != nil {
		return summary.Filters{}, err
	}
	return filtersFromQuery(q)
}

func parseQuery(raw string, extra ...string) (url.Values, error) {
	if len(raw) > 2048 {
		return nil, errors.New("query string is too long")
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return nil, errors.New("malformed query string")
	}
	for key, values := range q {
		switch key {
		case "season", "team", "down", "min_distance", "max_distance":
		default:
			allowed := false
			for _, name := range extra {
				allowed = allowed || key == name
			}
			if !allowed {
				return nil, errors.New("unknown query parameter: " + key)
			}
		}
		if len(values) != 1 || values[0] == "" {
			return nil, errors.New("query parameters must have one nonempty value: " + key)
		}
	}
	return q, nil
}

func filtersFromQuery(q url.Values) (summary.Filters, error) {
	f := summary.Filters{MinDistance: 0, MaxDistance: 100}
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

func parsePlayRequest(raw string) (plays.Request, error) {
	req := plays.Request{Limit: 25}
	q, err := parseQuery(raw, "limit", "offset", "source_sha256")
	if err != nil {
		return req, err
	}
	req.Filters, err = filtersFromQuery(q)
	if err != nil {
		return req, err
	}
	for _, param := range []struct {
		name  string
		value *int
	}{{"limit", &req.Limit}, {"offset", &req.Offset}} {
		if raw := q.Get(param.name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				return req, errors.New(param.name + " must be an integer")
			}
			*param.value = n
		}
	}
	req.SourceSHA256 = q.Get("source_sha256")
	return req, req.Validate()
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
