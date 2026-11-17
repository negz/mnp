package respond

import (
	"context"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/live"
)

func TestSinglesPoints(t *testing.T) {
	even := score{mu: 10, sigma: 0.85}
	strong := score{mu: 10 + 3, sigma: 0.85}
	weak := score{mu: 10 - 3, sigma: 0.85}

	cases := map[string]struct {
		reason string
		ours   score
		theirs score
		want   float64
	}{
		"Even": {
			reason: "Evenly matched players should expect half the 3 points.",
			ours:   even, theirs: even,
			want: 1.5,
		},
		"Strong": {
			reason: "A much stronger player should expect nearly 3 points.",
			ours:   strong, theirs: even,
			want: 3,
		},
		"Weak": {
			reason: "A much weaker player should expect nearly no points.",
			ours:   weak, theirs: even,
			want: 0,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := singlesPoints(tc.ours, tc.theirs)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateApprox(0, 0.05)); diff != "" {
				t.Errorf("\n%s\nsinglesPoints(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestDoublesPoints(t *testing.T) {
	even := score{mu: 10, sigma: 0.85}
	strong := score{mu: 10 + 3, sigma: 0.85}

	cases := map[string]struct {
		reason string
		ours   []score
		theirs []score
		want   float64
	}{
		"Even": {
			reason: "Evenly matched pairs should expect half the 5 points.",
			ours:   []score{even, even}, theirs: []score{even, even},
			want: 2.5,
		},
		"Strong": {
			reason: "A much stronger pair should expect nearly 5 points.",
			ours:   []score{strong, strong}, theirs: []score{even, even},
			want: 5,
		},
		"OneStrong": {
			reason: "One much stronger player should win both their points and the combined score, but the even player only half theirs.",
			ours:   []score{strong, even}, theirs: []score{even, even},
			want: 4,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := doublesPoints(tc.ours, tc.theirs)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateApprox(0, 0.05)); diff != "" {
				t.Errorf("\n%s\ndoublesPoints(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestEstimate(t *testing.T) {
	cases := map[string]struct {
		reason string
		p      Player
		league float64
		want   score
	}{
		"Unplayed": {
			reason: "A player who hasn't played a machine should be expected to play like others with their IPR.",
			p:      Player{IPR: 5},
			league: 100,
			want:   score{mu: math.Log(100) + 0.54, sigma: scoreSpread},
		},
		"ManyGames": {
			reason: "A player with many games should be expected to score close to their typical score.",
			p:      Player{IPR: 1, Games: 98, P50Score: 1000, P90Score: 1000 * math.Exp(scoreSpread*z90)},
			league: 100,
			want:   score{mu: (98*math.Log(1000) + 2*(math.Log(100)-0.27)) / 100, sigma: scoreSpread},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := estimate(tc.p, tc.league)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(score{}), cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nestimate(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestAssign(t *testing.T) {
	lineup := func(played ...int) []live.Player {
		ps := make([]live.Player, len(played))
		for i, n := range played {
			ps[i] = live.Player{NumPlayed: n}
		}
		return ps
	}
	// Player 0 is best at every game, by a lot on game 0 and a little on game
	// 1. The others are worth nothing.
	value := [][]float64{
		{3, 0, 0, 0},
		{1, 0.9, 0, 0},
	}
	singles := func(game int, players []int) float64 { return value[game][players[0]] }

	cases := map[string]struct {
		reason string
		lineup []live.Player
		round  int
		want   [][]int
	}{
		"BestOverall": {
			reason: "Player 0 should play where they're worth most, not on every game.",
			lineup: lineup(0, 0, 0, 0),
			round:  2,
			want:   [][]int{{0}, {1}},
		},
		"MustPlay": {
			reason: "Player 3 has sat out once, so they must play the rest of the match, even though player 1 is worth more.",
			lineup: lineup(2, 2, 2, 1),
			round:  3,
			want:   [][]int{{0}, {3}},
		},
		"MustPlayOther": {
			reason: "Players 2 and 3 have sat out once, so they must play the rest of the match.",
			lineup: lineup(2, 2, 1, 1),
			round:  3,
			want:   [][]int{{2}, {3}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := assign(2, tc.lineup, 1, tc.round, singles)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nassign(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestAssignFullLineup(t *testing.T) {
	// In a full lineup going into the last round, player 1 has played 2
	// games and everyone else 3. Player 0 can't play a 4th, though they'd be
	// worth the most.
	players := make([]live.Player, 10)
	for i := range players {
		players[i] = live.Player{NumPlayed: 3}
	}
	players[1].NumPlayed = 2
	points := func(_ int, ps []int) float64 {
		if ps[0] == 0 {
			return 3
		}
		return 1
	}
	got := assign(1, players, 1, 4, points)
	if diff := cmp.Diff([][]int{{1}}, got); diff != "" {
		t.Errorf("A player in a full lineup shouldn't play a 4th game: -want, +got:\n%s", diff)
	}
}

type fakeStore struct {
	stats map[string][]db.PlayerMachineStats
}

func (s *fakeStore) GetLeagueP50(_ context.Context) (map[string]float64, error) {
	return map[string]float64{"SternPark": 50_000_000}, nil
}

func (s *fakeStore) GetMachineNames(_ context.Context) (map[string]string, error) {
	return map[string]string{"SternPark": "Stern Jurassic Park"}, nil
}

func (s *fakeStore) GetSinglePlayerMachineStats(_ context.Context, name, _ string) ([]db.PlayerMachineStats, error) {
	return s.stats[name], nil
}

func TestAnalyze(t *testing.T) {
	m := &live.Match{
		Key:   "mnp-24-10-DRK-TTT",
		State: live.StateResponding,
		Round: 2,
		Home: live.Team{Key: "TTT", Lineup: []live.Player{
			{Key: "h1", Name: "Rick", IPR: 5},
			{Key: "h2", Name: "Mikey", IPR: 1},
		}},
		Away: live.Team{Key: "DRK", Lineup: []live.Player{
			{Key: "a1", Name: "Matt", IPR: 5},
		}},
		Rounds: []live.Round{
			{N: 2, Games: []live.Game{{N: 1, Machine: "SternPark", Player1: "h1"}}},
		},
	}
	// Round 2 is picked by the home team, so DRK responds to TTT's pick.
	s := &fakeStore{stats: map[string][]db.PlayerMachineStats{
		"Rick": {{MachineKey: "SternPark", Games: 36, P50Score: 190_000_000, P90Score: 367_000_000}},
		"Matt": {{MachineKey: "SternPark", Games: 10, P50Score: 83_000_000, P90Score: 260_000_000}},
	}}

	got, err := Analyze(context.Background(), s, m)
	if err != nil {
		t.Fatalf("Analyze(...): %v", err)
	}

	want := &Result{
		Match:     "mnp-24-10-DRK-TTT",
		Round:     2,
		Picker:    "TTT",
		Responder: "DRK",
		MaxPoints: 3,
		Games: []Game{{
			N: 1, MachineKey: "SternPark", MachineName: "Stern Jurassic Park", MaxPoints: 3,
			Theirs: []Player{{Name: "Rick", IPR: 5, Games: 36, P50Score: 190_000_000, P90Score: 367_000_000}},
			Suggested: []Option{{
				Player: Player{Name: "Matt", IPR: 5, Games: 10, P50Score: 83_000_000, P90Score: 260_000_000},
			}},
		}},
	}
	opts := []cmp.Option{
		cmpopts.IgnoreFields(Option{}, "Points"),
		cmpopts.IgnoreFields(Game{}, "Points"),
		cmpopts.IgnoreFields(Result{}, "Points"),
		cmpopts.EquateEmpty(),
	}
	if diff := cmp.Diff(want, got, opts...); diff != "" {
		t.Errorf("Analyze(...): -want, +got:\n%s", diff)
	}
	if got.Points <= 0 || got.Points >= 1.5 {
		t.Errorf("Analyze(...): Matt should expect fewer points than Rick, but more than none: got %.2f", got.Points)
	}
}
