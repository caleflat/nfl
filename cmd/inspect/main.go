// Command inspect profiles a local nflverse CSV before schema/import work.
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"

	"github.com/caleflat/nfl/internal/pbp"
)

var required = []string{
	"game_id", "play_id", "season", "season_type", "posteam", "play_type", "down",
	"qb_kneel", "qb_spike", "two_point_attempt", "play_deleted", "aborted_play",
	"yards_gained", "epa", "air_yards", "cpoe", "pass_attempt", "sack",
	"receiver_player_id", "third_down_converted", "third_down_failed",
	"fourth_down_converted", "fourth_down_failed",
}

type report struct {
	SHA256               string         `json:"sha256"`
	Rows                 int            `json:"rows"`
	Columns              int            `json:"columns"`
	Games                int            `json:"games"`
	DuplicateGamePlayIDs int            `json:"duplicate_game_play_ids"`
	Seasons              map[string]int `json:"seasons"`
	SeasonTypes          map[string]int `json:"season_types"`
	PlayTypes            map[string]int `json:"play_types"`
	RegularSeasonOffense offenseReport  `json:"regular_season_offense"`
}

type offenseReport struct {
	Plays                 int            `json:"plays"`
	Games                 int            `json:"games"`
	Teams                 map[string]int `json:"teams"`
	Missing               map[string]int `json:"missing"`
	TargetedAttempts      int            `json:"targeted_attempts"`
	TargetMissingAirYards int            `json:"target_missing_air_yards"`
	TargetMissingCPOE     int            `json:"target_missing_cpoe"`
	ThirdDown             conversion     `json:"third_down"`
	FourthDown            conversion     `json:"fourth_down"`
}

type conversion struct {
	Converted    int `json:"converted"`
	Failed       int `json:"failed"`
	Unclassified int `json:"unclassified"`
}

func main() {
	input := flag.String("file", "", "path to an uncompressed nflverse play-by-play CSV")
	flag.Parse()
	if err := run(*input, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "inspect:", err)
		os.Exit(1)
	}
}

func run(path string, out io.Writer) error {
	if path == "" {
		return errors.New("-file is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	result, err := inspect(io.TeeReader(f, h))
	if err != nil {
		return err
	}
	result.SHA256 = hex.EncodeToString(h.Sum(nil))
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func missing(value string) bool {
	return pbp.Missing(value)
}

func inspect(input io.Reader) (report, error) {
	result := report{
		Seasons: map[string]int{}, SeasonTypes: map[string]int{}, PlayTypes: map[string]int{},
		RegularSeasonOffense: offenseReport{Teams: map[string]int{}, Missing: map[string]int{
			"yards_gained": 0, "epa": 0, "air_yards": 0, "cpoe": 0,
		}},
	}
	r := csv.NewReader(input)
	header, err := r.Read()
	if err != nil {
		return result, fmt.Errorf("read header: %w", err)
	}
	columns := map[string]int{}
	for i, name := range header {
		if _, exists := columns[name]; exists {
			return result, fmt.Errorf("duplicate header %q", name)
		}
		columns[name] = i
	}
	for _, name := range required {
		if _, exists := columns[name]; !exists {
			return result, fmt.Errorf("missing required header %q", name)
		}
	}
	result.Columns = len(header)
	games, offenseGames := map[string]bool{}, map[string]bool{}
	seen := map[[2]string]bool{}
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return result, fmt.Errorf("record %d: %w", result.Rows+1, err)
		}
		result.Rows++
		get := func(name string) string { return record[columns[name]] }
		for _, name := range []string{"game_id", "play_id", "season", "season_type"} {
			if missing(get(name)) {
				return result, fmt.Errorf("record %d: missing %s", result.Rows, name)
			}
		}
		// Validate measured fields before reporting their coverage. Never treat
		// malformed values or infinities as ordinary missing observations.
		for _, name := range []string{"yards_gained", "epa", "air_yards", "cpoe"} {
			if missing(get(name)) {
				continue
			}
			n, err := strconv.ParseFloat(get(name), 64)
			if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
				return result, fmt.Errorf("record %d: invalid %s %q", result.Rows, name, get(name))
			}
		}
		games[get("game_id")] = true
		key := [2]string{get("game_id"), get("play_id")}
		if seen[key] {
			result.DuplicateGamePlayIDs++
		}
		seen[key] = true
		result.Seasons[get("season")]++
		result.SeasonTypes[get("season_type")]++
		result.PlayTypes[get("play_type")]++
		if !eligible(get) {
			continue
		}
		o := &result.RegularSeasonOffense
		o.Plays++
		o.Teams[get("posteam")]++
		offenseGames[get("game_id")] = true
		for name := range o.Missing {
			if missing(get(name)) {
				o.Missing[name]++
			}
		}
		if get("pass_attempt") == "1" && get("sack") == "0" && !missing(get("receiver_player_id")) {
			o.TargetedAttempts++
			if missing(get("air_yards")) {
				o.TargetMissingAirYards++
			}
			if missing(get("cpoe")) {
				o.TargetMissingCPOE++
			}
		}
		var c *conversion
		var prefix string
		switch get("down") {
		case "3":
			c, prefix = &o.ThirdDown, "third_down"
		case "4":
			c, prefix = &o.FourthDown, "fourth_down"
		}
		if c != nil {
			converted, failed := get(prefix+"_converted"), get(prefix+"_failed")
			switch {
			case converted == "1" && failed == "0":
				c.Converted++
			case converted == "0" && failed == "1":
				c.Failed++
			default:
				c.Unclassified++
			}
		}
	}
	result.Games, result.RegularSeasonOffense.Games = len(games), len(offenseGames)
	return result, nil
}

// This is a proposed analytical population, not an upstream NFL definition.
// Keep it synchronized with docs/DATA_CONTRACT.md while the contract is reviewed.
func eligible(get func(string) string) bool {
	return pbp.Eligible(get)
}
