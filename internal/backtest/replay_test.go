package backtest

import (
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/negz/mnp/internal/mnp"
	"github.com/negz/mnp/internal/points"
)

func TestReplay(t *testing.T) {
	// The home team picks rounds 2 and 4, and the away team 1 and 3. The
	// picking team plays as players 1 and 3.
	singlesIn := func(round int, machine string, picker, responder mnp.ResultData) mnp.GameData {
		picker.Position, responder.Position = 1, 2
		picker.IsHome = round%2 == 0
		responder.IsHome = !picker.IsHome
		return mnp.GameData{Round: round, MachineKey: machine, Results: []mnp.ResultData{picker, responder}}
	}
	singles := func(machine string, picker, responder mnp.ResultData) mnp.GameData {
		return singlesIn(2, machine, picker, responder)
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
					Picker:    []Player{{Name: "Alice", Known: points.Player{IPR: 3, Home: true, Picked: true}, Score: 100, Points: 0}},
					Responder: []Player{{Name: "Bob", Known: points.Player{IPR: 4}, Score: 400, Points: 3}},
				},
				{
					Season: 24, Date: "2026-09-15", Match: "m2", Round: 2, Machine: "TZ", League: 100,
					Picker: []Player{{
						Name:   "Alice",
						Known:  points.Player{IPR: 3, Home: true, Picked: true, Games: 1, P50: 100, LeagueGames: 1, Relative: 0},
						Score:  300,
						Points: 2,
					}},
					Responder: []Player{{
						Name:   "Bob",
						Known:  points.Player{IPR: 4, Games: 1, P50: 400, LeagueGames: 1, Relative: math.Log(4)},
						Score:  200,
						Points: 1,
					}},
				},
			},
		},
		"AwayPicks": {
			reason: "The away team's players should be the picking team's in rounds 1 and 3, and the home team's the responders.",
			seasons: []mnp.SeasonData{{
				Number: 24,
				Teams:  teams,
				Matches: []mnp.MatchData{
					{Key: "m1", Date: "2026-09-08", Games: []mnp.GameData{
						singlesIn(3, "TZ", mnp.ResultData{PlayerName: "Alice", IPR: 3, Score: 100}, mnp.ResultData{PlayerName: "Bob", IPR: 4, Score: 400}),
					}},
				},
			}},
			want: []Game{
				{
					Season: 24, Date: "2026-09-08", Match: "m1", Round: 3, Machine: "TZ",
					Picker:    []Player{{Name: "Alice", Known: points.Player{IPR: 3, Picked: true}, Score: 100}},
					Responder: []Player{{Name: "Bob", Known: points.Player{IPR: 4, Home: true}, Score: 400}},
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
					Picker:    []Player{{Name: "Alice", Known: points.Player{IPR: 3, Home: true, Picked: true}, Score: 100}},
					Responder: []Player{{Name: "Bob", Known: points.Player{IPR: 4}, Score: 400}},
				},
				{
					Season: 24, Date: "2026-09-08", Match: "m2", Round: 2, Machine: "TZ",
					Picker:    []Player{{Name: "Alice", Known: points.Player{IPR: 3, Home: true, Picked: true}, Score: 300}},
					Responder: []Player{{Name: "Bob", Known: points.Player{IPR: 4}, Score: 200}},
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
					Picker:    []Player{{Name: "Alice", Known: points.Player{IPR: 3, Home: true, Picked: true}, Score: 100}},
					Responder: []Player{{Name: "Carol", Known: points.Player{IPR: 1}, Score: 50}},
				},
				{
					Season: 24, Date: "2026-09-15", Match: "m2", Round: 2, Machine: "TZ", League: 100,
					Picker: []Player{{
						Name:  "Bob",
						Known: points.Player{IPR: 4, Home: true, Picked: true, LeagueGames: 1, Relative: 0},
						Score: 300,
					}},
					Responder: []Player{{
						Name:  "Carol",
						Known: points.Player{IPR: 1, Games: 1, P50: 50, LeagueGames: 1, Relative: math.Log(0.5)},
						Score: 200,
					}},
				},
				{
					Season: 24, Date: "2026-09-15", Match: "m2", Round: 2, Machine: "MM", League: 500,
					Picker: []Player{{
						Name:  "Alice",
						Known: points.Player{IPR: 3, Home: true, Picked: true, LeagueGames: 1, Relative: 0},
						Score: 300,
					}},
					Responder: []Player{{
						Name:  "Bob",
						Known: points.Player{IPR: 4, Games: 1, P50: 500, LeagueGames: 1, Relative: 0},
						Score: 200,
					}},
				},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := Replay(tc.seasons, 4)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nReplay(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
