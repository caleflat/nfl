// Package pbp reads the subset of nflverse fields used by contract version 1.
package pbp

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

type field struct {
	name, column, kind string
	required           bool
}

// Column order is shared by decoding and INSERT arguments. All identifiers are
// compiled into the program, never supplied by the input file or a caller.
var fields = []field{
	{"game_id", "game_id", "text", true}, {"play_id", "play_id", "int", true},
	{"season", "season", "int", true}, {"season_type", "season_type", "text", true},
	{"week", "week", "int", true}, {"game_date", "game_date", "date", true},
	{"home_team", "home_team", "text", true}, {"away_team", "away_team", "text", true},
	{"posteam", "posteam", "text", false}, {"defteam", "defteam", "text", false},
	{"drive", "drive", "int", false}, {"qtr", "qtr", "int", false},
	{"time", "time", "text", false}, {"down", "down", "int", false},
	{"ydstogo", "ydstogo", "int", false}, {"yardline_100", "yardline_100", "int", false},
	{"score_differential", "score_differential", "int", false},
	{"game_seconds_remaining", "game_seconds_remaining", "int", false},
	{"desc", "description", "text", false}, {"play_type", "play_type", "text", false},
	{"qb_kneel", "qb_kneel", "bool", false}, {"qb_spike", "qb_spike", "bool", false},
	{"qb_scramble", "qb_scramble", "bool", false}, {"qb_dropback", "qb_dropback", "bool", false},
	{"two_point_attempt", "two_point_attempt", "bool", false},
	{"play_deleted", "play_deleted", "bool", false}, {"aborted_play", "aborted_play", "bool", false},
	{"penalty", "penalty", "bool", false}, {"pass_attempt", "pass_attempt", "bool", false},
	{"sack", "sack", "bool", false}, {"receiver_player_id", "receiver_player_id", "text", false},
	{"yards_gained", "yards_gained", "int", false}, {"epa", "epa", "float", false},
	{"air_yards", "air_yards", "float", false}, {"cp", "cp", "float", false},
	{"cpoe", "cpoe", "float", false}, {"complete_pass", "complete_pass", "bool", false},
	{"interception", "interception", "bool", false}, {"touchdown", "touchdown", "bool", false},
	{"first_down", "first_down", "bool", false}, {"third_down_converted", "third_down_converted", "bool", false},
	{"third_down_failed", "third_down_failed", "bool", false},
	{"fourth_down_converted", "fourth_down_converted", "bool", false},
	{"fourth_down_failed", "fourth_down_failed", "bool", false},
}

var teams = strings.Fields("ARI ATL BAL BUF CAR CHI CIN CLE DAL DEN DET GB HOU IND JAX KC LA LAC LV MIA MIN NE NO NYG NYJ PHI PIT SEA SF TB TEN WAS")

type Play struct {
	Values          []any
	GameID, PlayID  string
	Season          int
	SeasonType      string
	Eligible        bool
	MissingDistance bool
}

type Decoder struct {
	r       *csv.Reader
	columns map[string]int
	row     int
}

func Missing(value string) bool { return value == "" || value == "NA" || value == "NaN" }

func Eligible(get func(string) string) bool {
	down := get("down")
	return get("season_type") == "REG" &&
		(get("play_type") == "pass" || get("play_type") == "run") &&
		!Missing(get("posteam")) &&
		(down == "1" || down == "2" || down == "3" || down == "4") &&
		get("qb_kneel") == "0" && get("qb_spike") == "0" &&
		get("two_point_attempt") == "0" && get("play_deleted") == "0" && get("aborted_play") == "0"
}

func Columns() []string {
	names := make([]string, 0, len(fields)+1)
	for _, f := range fields {
		names = append(names, f.column)
	}
	return append(names, "is_eligible")
}

func NewDecoder(input io.Reader) (*Decoder, error) {
	r := csv.NewReader(input)
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("CSV header: %w", err)
	}
	d := &Decoder{r: r, columns: map[string]int{}}
	for i, name := range header {
		if _, exists := d.columns[name]; exists {
			return nil, fmt.Errorf("duplicate header %q", name)
		}
		d.columns[name] = i
	}
	for _, f := range fields {
		if _, exists := d.columns[f.name]; !exists {
			return nil, fmt.Errorf("missing required header %q", f.name)
		}
	}
	return d, nil
}

func (d *Decoder) Next() (Play, error) {
	var p Play
	record, err := d.r.Read()
	if errors.Is(err, io.EOF) {
		return p, io.EOF
	}
	d.row++
	if err != nil {
		return p, fmt.Errorf("CSV record %d: %w", d.row, err)
	}
	get := func(name string) string { return record[d.columns[name]] }
	p.Values = make([]any, 0, len(fields)+1)
	for _, f := range fields {
		value, err := parse(f, get(f.name))
		if err != nil {
			return p, fmt.Errorf("CSV record %d, %s: %w", d.row, f.name, err)
		}
		p.Values = append(p.Values, value)
	}
	if get("play_type") == "pass" || get("play_type") == "run" {
		for _, name := range []string{"qb_kneel", "qb_spike", "two_point_attempt", "play_deleted", "aborted_play", "pass_attempt", "sack"} {
			if Missing(get(name)) {
				return p, fmt.Errorf("CSV record %d: missing classification flag %s on run/pass", d.row, name)
			}
		}
	}
	for _, name := range []string{"home_team", "away_team", "posteam", "defteam"} {
		value := get(name)
		if Missing(value) {
			continue
		}
		known := false
		for _, code := range teams {
			known = known || value == code
		}
		if !known {
			return p, fmt.Errorf("CSV record %d: unsupported team %q in %s", d.row, value, name)
		}
	}
	p.GameID = get("game_id")
	// Identity is numeric: differently padded strings must not evade duplicate
	// detection, including records skipped from storage.
	playID := p.Values[1].(int64)
	if playID < 0 {
		return p, fmt.Errorf("CSV record %d: play_id must be nonnegative", d.row)
	}
	p.PlayID = strconv.FormatInt(playID, 10)
	p.Season = int(p.Values[2].(int64))
	p.SeasonType = get("season_type")
	if p.SeasonType != "REG" && p.SeasonType != "POST" {
		return p, fmt.Errorf("CSV record %d: unsupported season_type %q", d.row, p.SeasonType)
	}
	if p.Season < 1999 || p.Season > 2100 {
		return p, fmt.Errorf("CSV record %d: season outside supported range", d.row)
	}
	p.Eligible = Eligible(get)
	p.MissingDistance = Missing(get("ydstogo"))
	p.Values = append(p.Values, p.Eligible)
	return p, nil
}

func parse(f field, raw string) (any, error) {
	if Missing(raw) {
		if f.required {
			return nil, errors.New("required value missing")
		}
		return nil, nil
	}
	switch f.kind {
	case "int":
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q", raw)
		}
		return n, nil
	case "float":
		n, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return nil, fmt.Errorf("invalid finite number %q", raw)
		}
		return n, nil
	case "bool":
		if raw != "0" && raw != "1" {
			return nil, fmt.Errorf("expected 0 or 1, got %q", raw)
		}
		return raw == "1", nil
	case "date":
		value, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return nil, fmt.Errorf("invalid date %q", raw)
		}
		return value, nil
	default:
		return raw, nil
	}
}
