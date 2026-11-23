package pick

import (
	"context"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/habit"
	"github.com/negz/mnp/internal/points"
)

func TestHabitual(t *testing.T) {
	// Everyone scores the same on the one machine, so each player has an
	// even chance against anyone.
	even := points.Score{Mu: 18, Sigma: 0.9}
	ours := scored{
		stats:  [][]Stats{{{IPR: 2}}, {{IPR: 6}}},
		scores: [][]points.Score{{even}, {even}},
	}
	theirs := scored{
		scores: [][]points.Score{{even}, {even}},
		habits: [][]habit.Features{{{IPR: 4, Games: 3, VenueGames: 1, Affinity: 0.5}}, {{IPR: 1}}},
	}

	cases := map[string]struct {
		reason string
		model  habit.Model
		ours   []int
		theirs []int
		want   float64
	}{
		"IPRMatch": {
			reason: "Sending an IPR 4 against an IPR 2 should be worth 4 x 2 IPR matches.",
			model:  habit.Model{IPRMatch: 1},
			ours:   []int{0},
			theirs: []int{0},
			want:   -8,
		},
		"IPRMatchPair": {
			reason: "Sending an IPR 4 against a pair of IPR 2 and 6 should face their average IPR of 4.",
			model:  habit.Model{IPRMatch: 1},
			ours:   []int{0, 1},
			theirs: []int{0},
			want:   missingCost - 16,
		},
		"Familiarity": {
			reason: "A player's games on the machine, anywhere and here, should count on a log scale.",
			model:  habit.Model{Games: 1, VenueGames: 2},
			ours:   []int{0},
			theirs: []int{0},
			want:   -math.Log(4) - 2*math.Log(2),
		},
		"Affinity": {
			reason: "How much better a player scores on the machine than usual should count.",
			model:  habit.Model{Affinity: 2},
			ours:   []int{0},
			theirs: []int{0},
			want:   -1,
		},
		"Win": {
			reason: "A player's chance against who they'd face should count.",
			model:  habit.Model{Win: 1},
			ours:   []int{0, 1},
			theirs: []int{0, 1},
			want:   -1,
		},
		"Missing": {
			reason: "Sending nobody should cost far more than sending anyone.",
			model:  habit.Model{Games: 1},
			ours:   []int{0},
			want:   missingCost,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := habitual(tc.model, ours, theirs)(0, tc.ours, tc.theirs)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nhabitual(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestScoreHabits(t *testing.T) {
	s := &fakeStore{
		machines: []string{"M1", "M2", "M3"},
		stats: map[string][]db.PlayerMachineStats{"A": {
			{MachineKey: "M1", Games: 8, P50Score: 200_000_000},
			{MachineKey: "M2", Games: 3, P50Score: 1}, // Mostly forfeits.
		}},
		venueStats: map[string][]db.PlayerMachineStats{"A": {
			{MachineKey: "M1", Games: 2, P50Score: 300_000_000},
		}},
		relative: map[string]db.RelativeScore{"A": {Games: 50, P50: 0.2}},
	}
	league, _ := s.GetLeagueP50(context.Background())

	got, err := score(context.Background(), s, league, "V", s.machines, []Player{{Name: "A", IPR: 3}}, true, false)
	if err != nil {
		t.Fatalf("score(...): %v", err)
	}
	want := [][]habit.Features{{
		{IPR: 3, Games: 8, VenueGames: 2, Affinity: math.Log(2) - 0.2},
		{IPR: 3, Games: 3},
		{IPR: 3},
	}}
	if diff := cmp.Diff(want, got.habits, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
		t.Errorf("score(...): what the habit model knows: -want, +got:\n%s", diff)
	}
}
