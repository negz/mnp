package backtest

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/negz/mnp/internal/mnp"
	"github.com/negz/mnp/internal/points"
)

func TestReplay(t *testing.T) {
	singles := func(machine string, picker, responder mnp.ResultData) mnp.GameData {
		picker.Position, responder.Position = 1, 2
		return mnp.GameData{Round: 2, MachineKey: machine, Results: []mnp.ResultData{picker, responder}}
	}
	teams := []mnp.TeamData{{Key: "AAA", Roster: []string{"Alice", "Bob"}}}

	cases := map[string]struct {
		reason  string
		seasons []mnp.SeasonData
		want    []Game
	}{
		"EarlierNightsAreEvidence": {
			reason: "Games from an earlier night should inform each player's stats and the league's typical score.",
			seasons: []mnp.SeasonData{{
				Number: 24,
				Teams:  teams,
				Matches: []mnp.MatchData{
					{Key: "m2", Date: "2026-09-15", Games: []mnp.GameData{
						singles("TZ", mnp.ResultData{PlayerName: "Alice", IPR: 3, Score: 300, Points: 2}, mnp.ResultData{PlayerName: "Bob", IPR: 4, Score: 200, Points: 1}),
					}},
					{Key: "m1", Date: "2026-09-08", Games: []mnp.GameData{
						singles("TZ", mnp.ResultData{PlayerName: "Alice", IPR: 3, Score: 100, Points: 0}, mnp.ResultData{PlayerName: "Bob", IPR: 4, Score: 400, Points: 3}),
					}},
				},
			}},
			want: []Game{
				{
					Season: 24, Date: "2026-09-08", Match: "m1", Round: 2, Machine: "TZ",
					Picker:    []Player{{Name: "Alice", Stats: points.Stats{IPR: 3}, Score: 100, Points: 0}},
					Responder: []Player{{Name: "Bob", Stats: points.Stats{IPR: 4}, Score: 400, Points: 3}},
				},
				{
					Season: 24, Date: "2026-09-15", Match: "m2", Round: 2, Machine: "TZ", League: 100,
					Picker:    []Player{{Name: "Alice", Stats: points.Stats{IPR: 3, Games: 1, P50: 100, P90: 100}, Score: 300, Points: 2}},
					Responder: []Player{{Name: "Bob", Stats: points.Stats{IPR: 4, Games: 1, P50: 400, P90: 400}, Score: 200, Points: 1}},
				},
			},
		},
		"SameNightIsNotEvidence": {
			reason: "Games from the same night shouldn't inform stats, because they weren't known when players were chosen.",
			seasons: []mnp.SeasonData{{
				Number: 24,
				Teams:  teams,
				Matches: []mnp.MatchData{
					{Key: "m1", Date: "2026-09-08", Games: []mnp.GameData{
						singles("TZ", mnp.ResultData{PlayerName: "Alice", IPR: 3, Score: 100}, mnp.ResultData{PlayerName: "Bob", IPR: 4, Score: 400}),
					}},
					{Key: "m2", Date: "2026-09-08", Games: []mnp.GameData{
						singles("TZ", mnp.ResultData{PlayerName: "Alice", IPR: 3, Score: 300}, mnp.ResultData{PlayerName: "Bob", IPR: 4, Score: 200}),
					}},
				},
			}},
			want: []Game{
				{
					Season: 24, Date: "2026-09-08", Match: "m1", Round: 2, Machine: "TZ",
					Picker:    []Player{{Name: "Alice", Stats: points.Stats{IPR: 3}, Score: 100}},
					Responder: []Player{{Name: "Bob", Stats: points.Stats{IPR: 4}, Score: 400}},
				},
				{
					Season: 24, Date: "2026-09-08", Match: "m2", Round: 2, Machine: "TZ",
					Picker:    []Player{{Name: "Alice", Stats: points.Stats{IPR: 3}, Score: 300}},
					Responder: []Player{{Name: "Bob", Stats: points.Stats{IPR: 4}, Score: 200}},
				},
			},
		},
		"LeagueIsRostered": {
			reason: "The league's typical score should only count players on the season's rosters, and forfeits should be neither replayed nor evidence.",
			seasons: []mnp.SeasonData{{
				Number: 24,
				Teams:  teams,
				Matches: []mnp.MatchData{
					{Key: "m1", Date: "2026-09-08", Games: []mnp.GameData{
						singles("TZ", mnp.ResultData{PlayerName: "Alice", IPR: 3, Score: 100}, mnp.ResultData{PlayerName: "Carol", IPR: 1, Score: 50}),
						singles("MM", mnp.ResultData{PlayerName: "Alice", IPR: 3, Score: 1}, mnp.ResultData{PlayerName: "Bob", IPR: 4, Score: 500}),
					}},
					{Key: "m2", Date: "2026-09-15", Games: []mnp.GameData{
						singles("TZ", mnp.ResultData{PlayerName: "Bob", IPR: 4, Score: 300}, mnp.ResultData{PlayerName: "Carol", IPR: 1, Score: 200}),
						singles("MM", mnp.ResultData{PlayerName: "Alice", IPR: 3, Score: 300}, mnp.ResultData{PlayerName: "Bob", IPR: 4, Score: 200}),
					}},
				},
			}},
			want: []Game{
				{
					Season: 24, Date: "2026-09-08", Match: "m1", Round: 2, Machine: "TZ",
					Picker:    []Player{{Name: "Alice", Stats: points.Stats{IPR: 3}, Score: 100}},
					Responder: []Player{{Name: "Carol", Stats: points.Stats{IPR: 1}, Score: 50}},
				},
				{
					Season: 24, Date: "2026-09-15", Match: "m2", Round: 2, Machine: "TZ", League: 100,
					Picker:    []Player{{Name: "Bob", Stats: points.Stats{IPR: 4}, Score: 300}},
					Responder: []Player{{Name: "Carol", Stats: points.Stats{IPR: 1, Games: 1, P50: 50, P90: 50}, Score: 200}},
				},
				{
					Season: 24, Date: "2026-09-15", Match: "m2", Round: 2, Machine: "MM", League: 500,
					Picker:    []Player{{Name: "Alice", Stats: points.Stats{IPR: 3}, Score: 300}},
					Responder: []Player{{Name: "Bob", Stats: points.Stats{IPR: 4, Games: 1, P50: 500, P90: 500}, Score: 200}},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := Replay(tc.seasons, 4)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nReplay(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
