package pbp

import (
	"encoding/csv"
	"strings"
	"testing"
)

func sample(t *testing.T, changes map[string]string) string {
	t.Helper()
	values := map[string]string{
		"game_id": "2025_01_NE_BUF", "play_id": "1", "season": "2025", "season_type": "REG",
		"week": "1", "game_date": "2025-09-07", "home_team": "BUF", "away_team": "NE",
		"posteam": "NE", "play_type": "pass", "down": "3", "ydstogo": "5",
		"qb_kneel": "0", "qb_spike": "0", "two_point_attempt": "0", "play_deleted": "0",
		"aborted_play": "0", "pass_attempt": "1", "sack": "0", "epa": "0", "air_yards": "-2",
	}
	for k, v := range changes {
		values[k] = v
	}
	var b strings.Builder
	w := csv.NewWriter(&b)
	header, row := []string{}, []string{}
	for _, f := range fields {
		header = append(header, f.name)
		row = append(row, values[f.name])
	}
	w.Write(header)
	w.Write(row)
	w.Flush()
	return b.String()
}

func TestDecodePreservesNullZeroAndNegative(t *testing.T) {
	d, err := NewDecoder(strings.NewReader(sample(t, nil)))
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.Next()
	if err != nil || !p.Eligible {
		t.Fatalf("decode: %+v %v", p, err)
	}
	values := map[string]any{}
	for i, column := range Columns() {
		values[column] = p.Values[i]
	}
	if values["yards_gained"] != nil || values["epa"] != float64(0) || values["air_yards"] != float64(-2) {
		t.Fatalf("null, zero, or negative changed: %+v", values)
	}
}

func TestDecodeRejectsInvalidDomainValues(t *testing.T) {
	for _, changes := range []map[string]string{
		{"epa": "Inf"}, {"cpoe": "garbage"}, {"play_id": "1.5"}, {"play_id": ""},
		{"qb_spike": "2"}, {"sack": ""}, {"posteam": "UNKNOWN"},
		{"game_date": "2025-02-30"}, {"season_type": "PRE"}, {"season": "2200"},
	} {
		d, err := NewDecoder(strings.NewReader(sample(t, changes)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Next(); err == nil {
			t.Fatalf("expected rejection: %+v", changes)
		}
	}
}

func TestDecodeAllowsAdministrativeNulls(t *testing.T) {
	d, err := NewDecoder(strings.NewReader(sample(t, map[string]string{
		"play_type": "", "down": "", "posteam": "", "sack": "", "qb_spike": "",
	})))
	if err != nil {
		t.Fatal(err)
	}
	p, err := d.Next()
	if err != nil || p.Eligible {
		t.Fatalf("administrative record: %+v %v", p, err)
	}
}
