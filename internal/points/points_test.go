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
		"MissingPlayer": {
			reason: "A missing player scores 1 and beats nobody, and their partner only wins the combined score by outscoring both opponents.",
			ours:   []Score{even}, theirs: []Score{even, even},
			want: 1 + 0.23,
		},
		"TheirMissingPlayer": {
			reason: "Against a missing player, a pair wins both of those points, half the others, and the combined score unless the other player outscores them both.",
			ours:   []Score{even, even}, theirs: []Score{even},
			want: 2 + 1 + (1 - 0.23),
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
		IPROffset:    [7]float64{-0.5, -0.4, 0, 0.2, 0.3, 0.6, 0.9},
		Spread:       0.9,
		LeagueGames:  20,
		MachineGames: 40,
		Unplayed:     0.1,
		Picked:       0.05,
		Home:         0.08,
	}

	cases := map[string]struct {
		reason string
		p      Player
		league float64
		want   Score
	}{
		"Unplayed": {
			reason: "A player who hasn't played the machine or the league should be expected to play like others with their IPR, less a bit for not knowing the machine.",
			p:      Player{IPR: 5},
			league: 100,
			want:   Score{Mu: math.Log(100) + 0.6 - 0.1, Sigma: 0.9},
		},
		"LeagueGames": {
			reason: "A player's league games should move their estimate toward how they score relative to the league.",
			p:      Player{IPR: 1, LeagueGames: 20, Relative: 0.5},
			league: 100,
			want:   Score{Mu: math.Log(100) + (20*0.5+20*-0.4)/40 - 0.1, Sigma: 0.9},
		},
		"MachineGames": {
			reason: "A player's games on the machine should move their estimate toward their typical score on it.",
			p:      Player{IPR: 1, Games: 40, P50: 1000},
			league: 100,
			want:   Score{Mu: (40*math.Log(1000) + 40*(math.Log(100)-0.4)) / 80, Sigma: 0.9},
		},
		"HomePicked": {
			reason: "A player at home on a machine their team picked should be expected to score higher.",
			p:      Player{IPR: 1, Home: true, Picked: true},
			league: 100,
			want:   Score{Mu: math.Log(100) - 0.4 - 0.1 + 0.05 + 0.08, Sigma: 0.9},
		},
		"NoLeague": {
			reason: "Without a typical score for the league, a player's own typical score should be used as is.",
			p:      Player{IPR: 1, Games: 3, P50: 1000},
			want:   Score{Mu: math.Log(1000), Sigma: 0.9},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := m.Score(tc.p, tc.league)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nScore(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestDoublesWin(t *testing.T) {
	even := Score{Mu: 10, Sigma: 0.9}
	strong := Score{Mu: 10 + 3, Sigma: 0.9}
	weak := Score{Mu: 10 - 3, Sigma: 0.9}

	cases := map[string]struct {
		reason string
		ours   []Score
		theirs []Score
		want   float64
	}{
		"Even": {
			reason: "Evenly matched pairs should each win half the time.",
			ours:   []Score{even, even}, theirs: []Score{even, even},
			want: 0.5,
		},
		"Strong": {
			reason: "A much stronger pair should nearly always win.",
			ours:   []Score{strong, strong}, theirs: []Score{even, even},
			want: 1,
		},
		"OneStrong": {
			reason: "One much stronger player wins both their points and the combined score, so their pair should nearly always win.",
			ours:   []Score{strong, even}, theirs: []Score{even, even},
			want: 0.98,
		},
		"MissingPlayer": {
			reason: "A missing player scores 1, so their partner has to outscore both opponents combined, as with a much weaker partner.",
			ours:   []Score{even}, theirs: []Score{even, even},
			want: 0.23,
		},
		"TheirMissingPlayer": {
			reason: "Against a missing player, a pair wins both of those points, and loses only if the other player outscores them both combined.",
			ours:   []Score{even, even}, theirs: []Score{even},
			want: 1 - 0.23,
		},
		"OneWeak": {
			reason: "With one much weaker player, a pair needs the other player to outscore both opponents combined, which an even player does about 23% of the time.",
			ours:   []Score{weak, even}, theirs: []Score{even, even},
			want: 0.23,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := DoublesWin(tc.ours, tc.theirs)
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateApprox(0, 0.02)); diff != "" {
				t.Errorf("\n%s\nDoublesWin(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
