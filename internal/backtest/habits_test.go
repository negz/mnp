package backtest

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/negz/mnp/internal/habit"
	"github.com/negz/mnp/internal/mnp"
	"github.com/negz/mnp/internal/points"
	"github.com/negz/mnp/internal/recency"
)

func TestResponses(t *testing.T) {
	// The home team picks round 2, and plays as player 1.
	singles := func(machine string, picker, responder mnp.ResultData) mnp.GameData {
		picker.Position, responder.Position = 1, 2
		picker.IsHome = true
		return mnp.GameData{Round: 2, MachineKey: machine, Results: []mnp.ResultData{picker, responder}}
	}
	alice := mnp.ResultData{PlayerName: "Alice", IPR: 3, Score: 100}
	carol := mnp.ResultData{PlayerName: "Carol", IPR: 1, Score: 100}
	bob := mnp.ResultData{PlayerName: "Bob", IPR: 4, Score: 400}
	dan := mnp.ResultData{PlayerName: "Dan", IPR: 5, Score: 400}
	forfeit := dan
	forfeit.Score = 1

	seasons := []mnp.SeasonData{{
		Number: 24,
		Matches: []mnp.MatchData{
			// A forfeit leaves the round out, but still counts as a game
			// on the machine.
			{Key: "m1", Date: "2026-09-08", Venue: mnp.VenueRef{Key: "V1"}, Games: []mnp.GameData{
				singles("TZ", alice, bob),
				singles("AFM", carol, forfeit),
			}},
			{Key: "m2", Date: "2026-09-08", Venue: mnp.VenueRef{Key: "V2"}, Games: []mnp.GameData{
				singles("TZ", carol, bob),
			}},
			{Key: "m3", Date: "2026-09-15", Venue: mnp.VenueRef{Key: "V1"}, Games: []mnp.GameData{
				singles("TZ", alice, dan),
				singles("AFM", carol, bob),
			}},
		},
	}}

	want := []Response{{
		Season: 24,
		Features: [][]habit.Features{
			// Dan, sent to TZ.
			{
				{IPR: 5, Facing: 3},
				{IPR: 5, Facing: 1, Games: 1, VenueGames: 1},
			},
			// Bob, sent to AFM.
			{
				{IPR: 4, Facing: 3, Games: 2, VenueGames: 1},
				{IPR: 4, Facing: 1},
			},
		},
		Sent: []int{0, 1},
	}}

	got := Responses(seasons, recency.ScoreHalfLife, points.DefaultModel())
	// Affinity, Win and Expected come from the points model.
	opts := []cmp.Option{
		cmpopts.IgnoreFields(habit.Features{}, "Affinity", "Win"),
		cmpopts.IgnoreFields(Response{}, "Expected"),
	}
	if diff := cmp.Diff(want, got, opts...); diff != "" {
		t.Errorf("Responses(...): -want, +got:\n%s", diff)
	}
}

func TestArrange(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	cases := map[string]struct {
		reason  string
		players int
		games   int
	}{
		"Singles": {
			reason:  "A singles response should send each player to the game that gets the most utility in total.",
			players: 7,
			games:   7,
		},
		"Doubles": {
			reason:  "A doubles response should send each pair to the game that gets the most utility in total.",
			players: 8,
			games:   4,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			utility := make([][]float64, tc.players)
			for p := range utility {
				utility[p] = make([]float64, tc.games)
				for g := range utility[p] {
					utility[p][g] = rng.Float64()
				}
			}
			r := Response{Sent: make([]int, tc.players), Features: make([][]habit.Features, tc.players)}
			for p := range r.Features {
				r.Features[p] = make([]habit.Features, tc.games)
			}

			// Try every order of players, filling games in turn.
			slots := tc.players / tc.games
			want := math.Inf(-1)
			order := make([]int, tc.players)
			for i := range order {
				order[i] = i
			}
			var permute func(k int)
			permute = func(k int) {
				if k == len(order) {
					var total float64
					for i, p := range order {
						total += utility[p][i/slots]
					}
					want = max(want, total)
					return
				}
				for i := k; i < len(order); i++ {
					order[k], order[i] = order[i], order[k]
					permute(k + 1)
					order[k], order[i] = order[i], order[k]
				}
			}
			permute(0)

			sent := arrange(r, func(p, g int) float64 { return utility[p][g] })
			var got float64
			for p, g := range sent {
				got += utility[p][g]
			}
			for g := range tc.games {
				if n := countGame(sent, g); n != slots {
					t.Errorf("\n%s\narrange(...): sent %d players to game %d, want %d", tc.reason, n, g, slots)
				}
			}
			if diff := cmp.Diff(want, got, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\narrange(...): -want most utility, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func countGame(sent []int, game int) int {
	n := 0
	for _, g := range sent {
		if g == game {
			n++
		}
	}
	return n
}

func TestFitHabits(t *testing.T) {
	// Captains who arrange their players as the model says they're likely
	// to, at random, should be fitted with the model they followed.
	want := habit.Model{IPRMatch: 0.1, Games: 0.9, VenueGames: 0.3, Affinity: 0.5, Win: 2}
	rng := rand.New(rand.NewPCG(3, 4))
	const rounds, players = 2000, 7

	rs := make([]Response, rounds)
	for i := range rs {
		r := Response{Features: make([][]habit.Features, players), Sent: rng.Perm(players)}
		for p := range r.Features {
			r.Features[p] = make([]habit.Features, players)
			for g := range r.Features[p] {
				r.Features[p][g] = habit.Features{
					IPR:        1 + rng.IntN(6),
					Facing:     1 + 5*rng.Float64(),
					Games:      rng.IntN(30),
					VenueGames: rng.IntN(10),
					Affinity:   rng.NormFloat64(),
					Win:        rng.Float64(),
				}
			}
		}
		// Swapping players as often as the model makes it likely leaves
		// each arrangement as likely as the model says it is.
		for range 500 {
			a, b := rng.IntN(players), rng.IntN(players)
			ga, gb := r.Sent[a], r.Sent[b]
			gain := want.Utility(r.Features[a][gb]) + want.Utility(r.Features[b][ga]) - want.Utility(r.Features[a][ga]) - want.Utility(r.Features[b][gb])
			if rng.Float64() < math.Exp(gain) {
				r.Sent[a], r.Sent[b] = gb, ga
			}
		}
		rs[i] = r
	}

	got := FitHabits(rs)
	if diff := cmp.Diff(want, got, cmpopts.EquateApprox(0, 0.15)); diff != "" {
		t.Errorf("FitHabits(...): -want, +got:\n%s", diff)
	}
}

func TestPlaced(t *testing.T) {
	// Two players were sent to games 1 and 0.
	rs := []Response{{
		Features: [][]habit.Features{{{}, {}}, {{}, {}}},
		Sent:     []int{1, 0},
	}}
	cases := map[string]struct {
		reason  string
		utility func(r Response, p, g int) float64
		want    float64
	}{
		"Right": {
			reason:  "A placement that prefers where players were sent should place them all.",
			utility: func(r Response, p, g int) float64 { return map[bool]float64{true: 1}[g == r.Sent[p]] },
			want:    1,
		},
		"Wrong": {
			reason:  "A placement that prefers where players weren't sent should place none of them.",
			utility: func(r Response, p, g int) float64 { return map[bool]float64{true: 1}[g != r.Sent[p]] },
			want:    0,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Placed(rs, tc.utility); got != tc.want {
				t.Errorf("\n%s\nPlaced(...): want %v, got %v", tc.reason, tc.want, got)
			}
		})
	}
}
