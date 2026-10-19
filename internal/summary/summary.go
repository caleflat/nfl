// Package summary computes situational metrics under data contract version 1.
package summary

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"time"
)

var (
	ErrSeasonUnavailable = errors.New("season has not been imported")
	ErrUnknownTeam       = errors.New("unknown team code")
)

type Filters struct {
	Season      int    `json:"season"`
	Team        string `json:"team"`
	Down        int    `json:"down"`
	MinDistance int    `json:"min_distance"`
	MaxDistance int    `json:"max_distance"`
}

func (f Filters) Validate() error {
	if f.Season < 1999 || f.Season > 2100 {
		return errors.New("season must be between 1999 and 2100")
	}
	if len(f.Team) < 2 || len(f.Team) > 3 {
		return errors.New("team must be a canonical uppercase team code")
	}
	for _, c := range f.Team {
		if c < 'A' || c > 'Z' {
			return errors.New("team must be a canonical uppercase team code")
		}
	}
	if f.Down < 1 || f.Down > 4 {
		return errors.New("down must be between 1 and 4")
	}
	if f.MinDistance < 0 || f.MaxDistance > 100 || f.MinDistance > f.MaxDistance {
		return errors.New("distance bounds must satisfy 0 <= min_distance <= max_distance <= 100")
	}
	return nil
}

type Dataset struct {
	Season          int       `json:"season"`
	PublishedAt     time.Time `json:"published_at"`
	SourceSHA256    string    `json:"source_sha256"`
	ContractVersion int       `json:"contract_version"`
}

// Mean exposes sufficient statistics, so consumers need not average averages.
// Missing counts are relative to the metric's population, not always all plays.
type Mean struct {
	Value   *float64 `json:"value"`
	Sum     *float64 `json:"sum"`
	Count   int64    `json:"count"`
	Missing int64    `json:"missing"`
}

type EPA struct {
	PerPlay         *float64 `json:"per_play"`
	Total           *float64 `json:"total"`
	Count           int64    `json:"count"`
	Missing         int64    `json:"missing"`
	SuccessfulPlays int64    `json:"successful_plays"`
	SuccessRate     *float64 `json:"success_rate"`
}

type Conversion struct {
	Value        *float64 `json:"value"`
	Converted    int64    `json:"converted"`
	Failed       int64    `json:"failed"`
	Classified   int64    `json:"classified"`
	Unclassified int64    `json:"unclassified"`
}

type Result struct {
	Filters          Filters     `json:"filters"`
	Dataset          Dataset     `json:"dataset"`
	PlayCount        int64       `json:"play_count"`
	YardsPerPlay     Mean        `json:"yards_per_play"`
	EPA              EPA         `json:"epa"`
	TargetedAttempts int64       `json:"identified_target_attempts"`
	ADOT             Mean        `json:"adot"`
	CPOE             Mean        `json:"cpoe"`
	ConversionRate   *Conversion `json:"conversion_rate"`
}

//go:embed summary.sql
var query string

type Store struct{ DB *sql.DB }

func (s Store) Summary(ctx context.Context, filters Filters) (Result, error) {
	result := Result{Filters: filters}
	if err := filters.Validate(); err != nil {
		return result, err
	}
	var published sql.NullTime
	var hash sql.NullString
	var version sql.NullInt64
	var knownTeam bool
	var yardCount, epaCount, successes, airCount, cpoeCount, converted, failed int64
	var yardSum, epaSum, airSum, cpoeSum sql.NullFloat64
	err := s.DB.QueryRowContext(ctx, query, filters.Season, filters.Team, filters.Down, filters.MinDistance, filters.MaxDistance).Scan(
		&published, &hash, &version, &knownTeam, &result.PlayCount,
		&yardCount, &yardSum, &epaCount, &epaSum, &successes,
		&result.TargetedAttempts, &airCount, &airSum, &cpoeCount, &cpoeSum, &converted, &failed,
	)
	if err != nil {
		return result, fmt.Errorf("situational summary: %w", err)
	}
	if !knownTeam {
		return result, ErrUnknownTeam
	}
	if !published.Valid {
		return result, ErrSeasonUnavailable
	}
	result.Dataset = Dataset{Season: filters.Season, PublishedAt: published.Time, SourceSHA256: hash.String, ContractVersion: int(version.Int64)}
	result.YardsPerPlay = mean(yardSum, yardCount, result.PlayCount)
	result.EPA = EPA{
		PerPlay: ratio(epaSum.Float64, epaCount), Total: nullable(epaSum), Count: epaCount,
		Missing: result.PlayCount - epaCount, SuccessfulPlays: successes, SuccessRate: ratio(float64(successes), epaCount),
	}
	result.ADOT = mean(airSum, airCount, result.TargetedAttempts)
	result.CPOE = mean(cpoeSum, cpoeCount, result.TargetedAttempts)
	if filters.Down == 3 || filters.Down == 4 {
		classified := converted + failed
		result.ConversionRate = &Conversion{
			Value: ratio(float64(converted), classified), Converted: converted, Failed: failed,
			Classified: classified, Unclassified: result.PlayCount - classified,
		}
	}
	return result, nil
}

func mean(sum sql.NullFloat64, count, population int64) Mean {
	return Mean{Value: ratio(sum.Float64, count), Sum: nullable(sum), Count: count, Missing: population - count}
}

func nullable(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	return &value.Float64
}

func ratio(numerator float64, denominator int64) *float64 {
	if denominator == 0 {
		return nil
	}
	value := numerator / float64(denominator)
	return &value
}
