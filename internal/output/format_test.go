package output

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestFormatScore(t *testing.T) {
	type args struct {
		score float64
	}
	type want struct {
		result string
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Billions": {
			reason: "Scores >= 1B should be formatted with B suffix.",
			args:   args{score: 2_500_000_000},
			want:   want{result: "2.5B"},
		},
		"Millions": {
			reason: "Scores >= 1M should be formatted with M suffix.",
			args:   args{score: 1_234_567},
			want:   want{result: "1.2M"},
		},
		"Thousands": {
			reason: "Scores >= 1K should be formatted with K suffix.",
			args:   args{score: 45_678},
			want:   want{result: "45.7K"},
		},
		"Small": {
			reason: "Scores < 1K should be formatted as plain integers.",
			args:   args{score: 999},
			want:   want{result: "999"},
		},
		"Zero": {
			reason: "Zero should be formatted as plain integer.",
			args:   args{score: 0},
			want:   want{result: "0"},
		},
		"ExactBoundaryMillions": {
			reason: "Exactly 1M should use M suffix.",
			args:   args{score: 1_000_000},
			want:   want{result: "1.0M"},
		},
		"ExactBoundaryThousands": {
			reason: "Exactly 1K should use K suffix.",
			args:   args{score: 1_000},
			want:   want{result: "1.0K"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := FormatScore(tc.args.score)
			if diff := cmp.Diff(tc.want.result, got); diff != "" {
				t.Errorf("\n%s\nFormatScore(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestFormatRelStr(t *testing.T) {
	type args struct {
		p50       float64
		leagueP50 float64
	}
	type want struct {
		result string
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Positive": {
			reason: "Above league average should show positive percentage.",
			args:   args{p50: 1_500_000, leagueP50: 1_000_000},
			want:   want{result: "(+50%)"},
		},
		"Negative": {
			reason: "Below league average should show negative percentage.",
			args:   args{p50: 750_000, leagueP50: 1_000_000},
			want:   want{result: "(-25%)"},
		},
		"Average": {
			reason: "Equal to league average should show (avg).",
			args:   args{p50: 1_000_000, leagueP50: 1_000_000},
			want:   want{result: "(avg)"},
		},
		"ZeroLeague": {
			reason: "Zero league P50 should return empty string.",
			args:   args{p50: 500_000, leagueP50: 0},
			want:   want{result: ""},
		},
		"RoundsToZero": {
			reason: "A tiny difference that rounds to zero should show (avg).",
			args:   args{p50: 1_000_001, leagueP50: 1_000_000},
			want:   want{result: "(avg)"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := FormatRelStr(tc.args.p50, tc.args.leagueP50)
			if diff := cmp.Diff(tc.want.result, got); diff != "" {
				t.Errorf("\n%s\nFormatRelStr(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestMachineName(t *testing.T) {
	type args struct {
		names map[string]string
		key   string
	}
	type want struct {
		result string
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Found": {
			reason: "Should return the full name when the key exists in the map.",
			args: args{
				names: map[string]string{"twd": "The Walking Dead", "got": "Game of Thrones"},
				key:   "twd",
			},
			want: want{result: "The Walking Dead"},
		},
		"NotFound": {
			reason: "Should fall back to the key itself when no name is found.",
			args: args{
				names: map[string]string{"twd": "The Walking Dead"},
				key:   "unknown",
			},
			want: want{result: "unknown"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := MachineName(tc.args.names, tc.args.key)
			if diff := cmp.Diff(tc.want.result, got); diff != "" {
				t.Errorf("\n%s\nMachineName(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
