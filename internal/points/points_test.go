package points

import (
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestSingles(t *testing.T) {
	even := Score{Mu: 10, Sigma: 0.85}
	strong := Score{Mu: 10 + 3, Sigma: 0.85}
	weak := Score{Mu: 10 - 3, Sigma: 0.85}

	cases := map[string]struct {
		reason string
		ours   Score
		theirs Score
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
			got := Singles(tc.ours, tc.theirs)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateApprox(0, 0.05)); diff != "" {
				t.Errorf("\n%s\nSingles(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestDoubles(t *testing.T) {
	even := Score{Mu: 10, Sigma: 0.85}
	strong := Score{Mu: 10 + 3, Sigma: 0.85}

	cases := map[string]struct {
		reason string
		ours   []Score
		theirs []Score
		want   float64
	}{
		"Even": {
			reason: "Evenly matched pairs should expect half the 5 points.",
			ours:   []Score{even, even}, theirs: []Score{even, even},
			want: 2.5,
		},
		"Strong": {
			reason: "A much stronger pair should expect nearly 5 points.",
			ours:   []Score{strong, strong}, theirs: []Score{even, even},
			want: 5,
		},
		"OneStrong": {
			reason: "One much stronger player should win both their points and the combined score, but the even player only half theirs.",
			ours:   []Score{strong, even}, theirs: []Score{even, even},
			want: 4,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := Doubles(tc.ours, tc.theirs)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateApprox(0, 0.05)); diff != "" {
				t.Errorf("\n%s\nDoubles(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestScore(t *testing.T) {
	m := Model{
		IPROffset:        [7]float64{0, -0.27, -0.02, 0.14, 0.28, 0.54, 0.96},
		Spread:           0.85,
		PriorGames:       2,
		PriorSpreadGames: 5,
	}

	cases := map[string]struct {
		reason string
		s      Stats
		league float64
		want   Score
	}{
		"Unplayed": {
			reason: "A player who hasn't played a machine should be expected to play like others with their IPR.",
			s:      Stats{IPR: 5},
			league: 100,
			want:   Score{Mu: math.Log(100) + 0.54, Sigma: 0.85},
		},
		"ManyGames": {
			reason: "A player with many games should be expected to score close to their typical score.",
			s:      Stats{IPR: 1, Games: 98, P50: 1000, P90: 1000 * math.Exp(0.85*z90)},
			league: 100,
			want:   Score{Mu: (98*math.Log(1000) + 2*(math.Log(100)-0.27)) / 100, Sigma: 0.85},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := m.Score(tc.s, tc.league)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nScore(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
