// Package plays retrieves the records behind a situational summary.
package plays

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caleflat/nfl/internal/summary"
)

var ErrDatasetChanged = errors.New("dataset changed; restart from the first page with the current summary")

type Request struct {
	Filters      summary.Filters
	Limit        int
	Offset       int
	SourceSHA256 string
}

func (r Request) Validate() error {
	if err := r.Filters.Validate(); err != nil {
		return err
	}
	if r.Limit < 1 || r.Limit > 100 || r.Offset < 0 || r.Offset > 100000 {
		return errors.New("limit must be 1–100 and offset must be 0–100000")
	}
	if r.SourceSHA256 != "" {
		if _, err := hex.DecodeString(r.SourceSHA256); err != nil || len(r.SourceSHA256) != 64 || r.SourceSHA256 != strings.ToLower(r.SourceSHA256) {
			return errors.New("source_sha256 must be 64 lowercase hexadecimal characters")
		}
	}
	return nil
}

type Play struct {
	GameID            string   `json:"game_id"`
	PlayID            int      `json:"play_id"`
	Week              int      `json:"week"`
	GameDate          string   `json:"game_date"`
	HomeTeam          string   `json:"home_team"`
	AwayTeam          string   `json:"away_team"`
	Offense           string   `json:"offense"`
	Defense           *string  `json:"defense"`
	Quarter           *int     `json:"quarter"`
	Clock             *string  `json:"clock"`
	Down              int      `json:"down"`
	YardsToGo         int      `json:"yards_to_go"`
	Yardline100       *int     `json:"yardline_100"`
	ScoreDifferential *int     `json:"score_differential"`
	Description       *string  `json:"description"`
	PlayType          *string  `json:"play_type"`
	YardsGained       *int     `json:"yards_gained"`
	EPA               *float64 `json:"epa"`
	AirYards          *float64 `json:"air_yards"`
	CPOE              *float64 `json:"cpoe"`
	IdentifiedTarget  bool     `json:"identified_target"`
	Sack              *bool    `json:"sack"`
	QBScramble        *bool    `json:"qb_scramble"`
	Penalty           *bool    `json:"penalty"`
	CompletePass      *bool    `json:"complete_pass"`
	Interception      *bool    `json:"interception"`
	Touchdown         *bool    `json:"touchdown"`
	ConversionStatus  string   `json:"conversion_status"`
}

type Pagination struct {
	Limit      int   `json:"limit"`
	Offset     int   `json:"offset"`
	Total      int64 `json:"total"`
	NextOffset *int  `json:"next_offset"`
}

type Result struct {
	Filters    summary.Filters `json:"filters"`
	Dataset    summary.Dataset `json:"dataset"`
	Pagination Pagination      `json:"pagination"`
	Plays      []Play          `json:"plays"`
}

const predicate = `season=$1 AND posteam=$2 AND down=$3 AND is_eligible AND ydstogo BETWEEN $4 AND $5`

const pageQuery = `SELECT game_id,play_id,week,game_date,home_team,away_team,posteam,defteam,
    qtr,time,down,ydstogo,yardline_100,score_differential,description,play_type,
    yards_gained,epa,air_yards,cpoe,
    pass_attempt IS TRUE AND sack IS FALSE AND receiver_player_id IS NOT NULL,
    sack,qb_scramble,penalty,complete_pass,interception,touchdown,
    CASE
        WHEN down=3 AND third_down_converted IS TRUE AND third_down_failed IS FALSE THEN 'converted'
        WHEN down=3 AND third_down_failed IS TRUE AND third_down_converted IS FALSE THEN 'failed'
        WHEN down=4 AND fourth_down_converted IS TRUE AND fourth_down_failed IS FALSE THEN 'converted'
        WHEN down=4 AND fourth_down_failed IS TRUE AND fourth_down_converted IS FALSE THEN 'failed'
        WHEN down IN (3,4) THEN 'unclassified'
        ELSE 'not_applicable'
    END
FROM plays WHERE ` + predicate + ` ORDER BY game_id,play_id LIMIT $6 OFFSET $7`

type Store struct{ DB *sql.DB }

func (s Store) Plays(ctx context.Context, req Request) (Result, error) {
	result := Result{Filters: req.Filters, Pagination: Pagination{Limit: req.Limit, Offset: req.Offset}, Plays: []Play{}}
	if err := req.Validate(); err != nil {
		return result, err
	}
	// Metadata, count, and page must observe the same publication. PostgreSQL
	// retains that snapshot even if a concurrent reimport commits between reads.
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var known bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM teams WHERE code=$1)`, req.Filters.Team).Scan(&known); err != nil {
		return result, err
	}
	if !known {
		return result, summary.ErrUnknownTeam
	}
	result.Dataset.Season = req.Filters.Season
	err = tx.QueryRowContext(ctx, `SELECT published_at,source_sha256,contract_version FROM season_imports WHERE season=$1`, req.Filters.Season).
		Scan(&result.Dataset.PublishedAt, &result.Dataset.SourceSHA256, &result.Dataset.ContractVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return result, summary.ErrSeasonUnavailable
	}
	if err != nil {
		return result, err
	}
	if req.SourceSHA256 != "" && req.SourceSHA256 != result.Dataset.SourceSHA256 {
		return result, ErrDatasetChanged
	}
	args := []any{req.Filters.Season, req.Filters.Team, req.Filters.Down, req.Filters.MinDistance, req.Filters.MaxDistance}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM plays WHERE `+predicate, args...).Scan(&result.Pagination.Total); err != nil {
		return result, err
	}
	rows, err := tx.QueryContext(ctx, pageQuery, append(args, req.Limit, req.Offset)...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var p Play
		var date time.Time
		if err := rows.Scan(&p.GameID, &p.PlayID, &p.Week, &date, &p.HomeTeam, &p.AwayTeam, &p.Offense, &p.Defense,
			&p.Quarter, &p.Clock, &p.Down, &p.YardsToGo, &p.Yardline100, &p.ScoreDifferential, &p.Description, &p.PlayType,
			&p.YardsGained, &p.EPA, &p.AirYards, &p.CPOE, &p.IdentifiedTarget, &p.Sack, &p.QBScramble,
			&p.Penalty, &p.CompletePass, &p.Interception, &p.Touchdown, &p.ConversionStatus); err != nil {
			return result, fmt.Errorf("matching play: %w", err)
		}
		p.GameDate = date.Format("2006-01-02")
		result.Plays = append(result.Plays, p)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	if err := rows.Close(); err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	next := req.Offset + len(result.Plays)
	if int64(next) < result.Pagination.Total {
		result.Pagination.NextOffset = &next
	}
	return result, nil
}
