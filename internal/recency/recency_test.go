package recency

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestPercentile(t *testing.T) {
	type args struct {
		games  []Game
		latest int
		p      float64
	}

	cases := map[string]struct {
		reason string
		args   args
		want   float64
	}{
		"NoGames": {
			reason: "There's no percentile of no games.",
			args:   args{latest: 24, p: 0.5},
			want:   0,
		},
		"MedianOfThree": {
			reason: "The median of three games from one season should be the second lowest score.",
			args: args{
				games:  []Game{{24, 300}, {24, 100}, {24, 200}},
				latest: 24,
				p:      0.5,
			},
			want: 200,
		},
		"MedianOfTwo": {
			reason: "The median of two games from one season should be the lower score.",
			args: args{
				games:  []Game{{24, 200}, {24, 100}},
				latest: 24,
				p:      0.5,
			},
			want: 100,
		},
		"P90OfTen": {
			reason: "The 90th percentile of ten games from one season should be the ninth lowest score.",
			args: args{
				games: []Game{
					{24, 1},
					{24, 2},
					{24, 3},
					{24, 4},
					{24, 5},
					{24, 6},
					{24, 7},
					{24, 8},
					{24, 9},
					{24, 10},
				},
				latest: 24,
				p:      0.9,
			},
			want: 9,
		},
		"MedianOfSixFromLastSeason": {
			reason: "The median of six games from an older season should still be the third lowest score, despite floating point error in their weights.",
			args: args{
				games:  []Game{{23, 6}, {23, 5}, {23, 4}, {23, 3}, {23, 2}, {23, 1}},
				latest: 24,
				p:      0.5,
			},
			want: 3,
		},
		"OlderGamesCountLess": {
			reason: "Two games from eight seasons ago should count for less than one recent game.",
			args: args{
				games:  []Game{{16, 100}, {16, 100}, {24, 500}},
				latest: 24,
				p:      0.5,
			},
			want: 500,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := Percentile(tc.args.games, tc.args.latest, ScoreHalfLife, tc.args.p)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nPercentile(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestPlays(t *testing.T) {
	cases := map[string]struct {
		reason string
		games  []Game
		want   float64
	}{
		"ThisSeason": {
			reason: "Games from the latest season should count once each.",
			games:  []Game{{24, 1}, {24, 2}},
			want:   2,
		},
		"LastSeason": {
			reason: "A game from last season should count a quarter as much as one from this season.",
			games:  []Game{{24, 1}, {23, 2}},
			want:   1.25,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := Plays(tc.games, 24, PlayHalfLife)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nPlays(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
