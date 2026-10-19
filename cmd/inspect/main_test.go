package main

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

func fixture(t *testing.T, overrides ...map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(required); err != nil {
		t.Fatal(err)
	}
	for _, override := range overrides {
		values := map[string]string{
			"game_id": "2025_01_NE_BUF", "play_id": "1", "season": "2025", "season_type": "REG",
			"posteam": "NE", "play_type": "pass", "down": "3", "qb_kneel": "0", "qb_spike": "0",
			"two_point_attempt": "0", "play_deleted": "0", "aborted_play": "0",
			"yards_gained": "0", "epa": "-0.5", "air_yards": "-2", "cpoe": "-60",
			"pass_attempt": "1", "sack": "0", "receiver_player_id": "receiver-1",
			"third_down_converted": "0", "third_down_failed": "1",
			"fourth_down_converted": "0", "fourth_down_failed": "0",
		}
		for k, v := range override {
			values[k] = v
		}
		row := make([]string, len(required))
		for i, name := range required {
			row[i] = values[name]
		}
		if err := w.Write(row); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestInspectCoverageAndConversionClassification(t *testing.T) {
	source := fixture(t,
		map[string]string{}, // Negative target depth is valid.
		map[string]string{"play_id": "2", "air_yards": "0", "epa": "0", "third_down_converted": "1", "third_down_failed": "0"},
		map[string]string{"play_id": "3", "sack": "1", "receiver_player_id": "", "air_yards": "", "cpoe": "", "third_down_failed": "0"},
		map[string]string{"play_id": "4", "epa": "NA", "air_yards": "NaN", "cpoe": ""},
		map[string]string{"play_id": "5", "season_type": "POST"},
		map[string]string{"play_id": "6", "play_type": "no_play"},
		map[string]string{"play_id": "7", "aborted_play": "1"},
		map[string]string{"play_id": "8", "play_deleted": "1"},
		map[string]string{"play_id": "9", "qb_kneel": "1"},
		map[string]string{"play_id": "10", "qb_spike": "1"},
		map[string]string{"play_id": "11", "two_point_attempt": "1"},
	)
	r, err := inspect(strings.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	o := r.RegularSeasonOffense
	if r.Rows != 11 || r.Games != 1 || o.Plays != 4 || o.TargetedAttempts != 3 {
		t.Fatalf("unexpected counts: %+v", r)
	}
	if o.Missing["air_yards"] != 2 || o.Missing["epa"] != 1 || o.TargetMissingAirYards != 1 || o.TargetMissingCPOE != 1 {
		t.Fatalf("unexpected missing coverage: %+v", o)
	}
	if o.ThirdDown != (conversion{Converted: 1, Failed: 2, Unclassified: 1}) {
		t.Fatalf("unclassified play must not become a failure: %+v", o.ThirdDown)
	}
}

func TestInspectDetectsDuplicateIdentity(t *testing.T) {
	r, err := inspect(strings.NewReader(fixture(t, map[string]string{}, map[string]string{})))
	if err != nil || r.DuplicateGamePlayIDs != 1 {
		t.Fatalf("duplicate detection: %+v, %v", r, err)
	}
}

func TestInspectRejectsInvalidInput(t *testing.T) {
	for _, tc := range []struct{ name, input string }{
		{"missing header", "game_id,play_id\na,1\n"},
		{"duplicate header", "game_id,game_id\na,a\n"},
		{"bad number", fixture(t, map[string]string{"epa": "bad"})},
		{"infinity", fixture(t, map[string]string{"epa": "Inf"})},
		{"missing identity", fixture(t, map[string]string{"play_id": ""})},
		{"malformed row", fixture(t) + "too,few,columns\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := inspect(strings.NewReader(tc.input)); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
