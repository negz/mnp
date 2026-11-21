package pick

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/negz/mnp/internal/db"
)

type fakeStore struct {
	machines []string
	stats    map[string][]db.PlayerMachineStats
	relative map[string]db.RelativeScore
}

func (s *fakeStore) GetLeagueP50(_ context.Context) (map[string]float64, error) {
	league := make(map[string]float64)
	for _, m := range s.machines {
		league[m] = 100_000_000
	}
	return league, nil
}

func (s *fakeStore) GetMachineNames(_ context.Context) (map[string]string, error) {
	names := make(map[string]string)
	for _, m := range s.machines {
		names[m] = "Machine " + m
	}
	return names, nil
}

func (s *fakeStore) GetSinglePlayerMachineStats(_ context.Context, name, _ string) ([]db.PlayerMachineStats, error) {
	return s.stats[name], nil
}

func (s *fakeStore) GetRelativeScore(_ context.Context, name string, _ map[string]float64) (db.RelativeScore, error) {
	return s.relative[name], nil
}

func TestAnalyze(t *testing.T) {
	// A specialist is far better than the league on one machine, and plays
	// like an average IPR 3 everywhere else.
	specialist := func(machine string, p50 float64) []db.PlayerMachineStats {
		return []db.PlayerMachineStats{{MachineKey: machine, Games: 40, P50Score: p50, P90Score: 2 * p50}}
	}
	weak := db.RelativeScore{Games: 100, P50: -2}

	cases := map[string]struct {
		reason string
		round  Round
		store  *fakeStore
		want   *Result
	}{
		"Singles": {
			reason: "Each of our specialists should play their machine, against theirs, and our weak player should sit out.",
			round: Round{
				N:        2,
				Home:     true,
				Machines: []string{"M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8"},
				Ours:     []Player{{Name: "A1", IPR: 3}, {Name: "A2", IPR: 3}, {Name: "A3", IPR: 3}, {Name: "A4", IPR: 3}, {Name: "A5", IPR: 3}, {Name: "A6", IPR: 3}, {Name: "A7", IPR: 3}, {Name: "A8", IPR: 3}},
				Theirs:   []Player{{Name: "B1", IPR: 3}, {Name: "B2", IPR: 3}, {Name: "B3", IPR: 3}, {Name: "B4", IPR: 3}, {Name: "B5", IPR: 3}, {Name: "B6", IPR: 3}, {Name: "B7", IPR: 3}},
			},
			store: &fakeStore{
				machines: []string{"M1", "M2", "M3", "M4", "M5", "M6", "M7", "M8"},
				stats: func() map[string][]db.PlayerMachineStats {
					stats := make(map[string][]db.PlayerMachineStats)
					for i := 1; i <= 7; i++ {
						stats[fmt.Sprintf("A%d", i)] = specialist(fmt.Sprintf("M%d", i), 1_000_000_000)
						stats[fmt.Sprintf("B%d", i)] = specialist(fmt.Sprintf("M%d", i), 600_000_000)
					}
					return stats
				}(),
				relative: map[string]db.RelativeScore{"A8": weak},
			},
			want: &Result{
				Round:     2,
				MaxPoints: 21,
				Games: func() []Game {
					var games []Game
					for i := 1; i <= 7; i++ {
						games = append(games, Game{
							MachineKey:  fmt.Sprintf("M%d", i),
							MachineName: fmt.Sprintf("Machine M%d", i),
							Ours:        []Stats{{Name: fmt.Sprintf("A%d", i), IPR: 3, Games: 40, P50Score: 1_000_000_000, P90Score: 2_000_000_000}},
							Theirs:      []Stats{{Name: fmt.Sprintf("B%d", i), IPR: 3, Games: 40, P50Score: 600_000_000, P90Score: 1_200_000_000}},
							MaxPoints:   3,
						})
					}
					return games
				}(),
				SittingOut: []Player{{Name: "A8", IPR: 3}},
				Unpicked:   []string{"Machine M8"},
			},
		},
		"Doubles": {
			reason: "Each of our specialist pairs should play their machine, against theirs, and our weak player should sit out.",
			round: Round{
				N:        1,
				Machines: []string{"M1", "M2", "M3", "M4", "M5"},
				Ours:     []Player{{Name: "A1"}, {Name: "A2"}, {Name: "A3"}, {Name: "A4"}, {Name: "A5"}, {Name: "A6"}, {Name: "A7"}, {Name: "A8"}, {Name: "A9"}},
				Theirs:   []Player{{Name: "B1"}, {Name: "B2"}, {Name: "B3"}, {Name: "B4"}, {Name: "B5"}, {Name: "B6"}, {Name: "B7"}, {Name: "B8"}},
			},
			store: &fakeStore{
				machines: []string{"M1", "M2", "M3", "M4", "M5"},
				stats: func() map[string][]db.PlayerMachineStats {
					stats := make(map[string][]db.PlayerMachineStats)
					for i := 1; i <= 8; i++ {
						stats[fmt.Sprintf("A%d", i)] = specialist(fmt.Sprintf("M%d", (i+1)/2), 1_000_000_000)
						stats[fmt.Sprintf("B%d", i)] = specialist(fmt.Sprintf("M%d", (i+1)/2), 600_000_000)
					}
					return stats
				}(),
				relative: map[string]db.RelativeScore{"A9": weak},
			},
			want: &Result{
				Round:     1,
				Doubles:   true,
				MaxPoints: 20,
				Games: func() []Game {
					var games []Game
					for i := 1; i <= 4; i++ {
						games = append(games, Game{
							MachineKey:  fmt.Sprintf("M%d", i),
							MachineName: fmt.Sprintf("Machine M%d", i),
							Ours: []Stats{
								{Name: fmt.Sprintf("A%d", 2*i-1), Games: 40, P50Score: 1_000_000_000, P90Score: 2_000_000_000},
								{Name: fmt.Sprintf("A%d", 2*i), Games: 40, P50Score: 1_000_000_000, P90Score: 2_000_000_000},
							},
							Theirs: []Stats{
								{Name: fmt.Sprintf("B%d", 2*i-1), Games: 40, P50Score: 600_000_000, P90Score: 1_200_000_000},
								{Name: fmt.Sprintf("B%d", 2*i), Games: 40, P50Score: 600_000_000, P90Score: 1_200_000_000},
							},
							MaxPoints: 5,
						})
					}
					return games
				}(),
				SittingOut: []Player{{Name: "A9"}},
				Unpicked:   []string{"Machine M5"},
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Analyze(context.Background(), tc.store, tc.round)
			if err != nil {
				t.Fatalf("Analyze(...): %v", err)
			}
			opts := []cmp.Option{
				cmpopts.IgnoreFields(Game{}, "Points", "Win"),
				cmpopts.IgnoreFields(Result{}, "Points"),
				// Games are in order of the chance to win them, which
				// these cases don't pin down.
				cmpopts.SortSlices(func(a, b Game) bool { return a.MachineKey < b.MachineKey }),
				// Pairs are in either order.
				cmpopts.SortSlices(func(a, b Stats) bool { return a.Name < b.Name }),
			}
			if diff := cmp.Diff(tc.want, got, opts...); diff != "" {
				t.Errorf("\n%s\nAnalyze(...): -want, +got:\n%s", tc.reason, diff)
			}
			var total float64
			for _, g := range got.Games {
				total += g.Points
				if g.Win <= 0.5 {
					t.Errorf("\n%s\nAnalyze(...): our specialists should be favoured on %s, got a %.0f%% chance to win", tc.reason, g.MachineKey, g.Win*100)
				}
			}
			if diff := cmp.Diff(total, got.Points, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nAnalyze(...): round points should add up its games' points: -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestAnalyzeErrors(t *testing.T) {
	players := func(n int) []Player {
		ps := make([]Player, n)
		for i := range ps {
			ps[i] = Player{Name: fmt.Sprintf("P%d", i)}
		}
		return ps
	}
	cases := map[string]struct {
		reason string
		round  Round
	}{
		"TooFewMachines": {
			reason: "A singles round can't be picked from fewer than 7 machines.",
			round:  Round{N: 2, Machines: []string{"M1", "M2"}, Ours: players(7), Theirs: players(7)},
		},
		"TooFewPlayers": {
			reason: "A doubles round can't be picked with fewer than 8 players.",
			round:  Round{N: 4, Machines: []string{"M1", "M2", "M3", "M4"}, Ours: players(7), Theirs: players(8)},
		},
		"PinnedMachineCantBePicked": {
			reason: "A machine that can't be picked can't be pinned.",
			round:  Round{N: 4, Machines: []string{"M1", "M2", "M3", "M4"}, Ours: players(8), Theirs: players(8), Pinned: []Pin{{Machine: "M9", Players: []string{"P0", "P1"}}}},
		},
		"PinnedPlayerCantPlay": {
			reason: "A player who can't play can't be pinned.",
			round:  Round{N: 4, Machines: []string{"M1", "M2", "M3", "M4"}, Ours: players(8), Theirs: players(8), Pinned: []Pin{{Machine: "M1", Players: []string{"P0", "P9"}}}},
		},
		"PinnedPlayerTwice": {
			reason: "A player can't be pinned to two games.",
			round: Round{N: 4, Machines: []string{"M1", "M2", "M3", "M4"}, Ours: players(8), Theirs: players(8), Pinned: []Pin{
				{Machine: "M1", Players: []string{"P0", "P1"}},
				{Machine: "M2", Players: []string{"P1", "P2"}},
			}},
		},
		"PinnedMachineTwice": {
			reason: "A machine can't be pinned to two games.",
			round: Round{N: 4, Machines: []string{"M1", "M2", "M3", "M4"}, Ours: players(8), Theirs: players(8), Pinned: []Pin{
				{Machine: "M1", Players: []string{"P0", "P1"}},
				{Machine: "M1", Players: []string{"P2", "P3"}},
			}},
		},
		"PinnedTooFewPlayers": {
			reason: "A doubles game can't be pinned to one player.",
			round:  Round{N: 4, Machines: []string{"M1", "M2", "M3", "M4"}, Ours: players(8), Theirs: players(8), Pinned: []Pin{{Machine: "M1", Players: []string{"P0"}}}},
		},
		"LineupTooBig": {
			reason: "A lineup can't have more than 10 players.",
			round:  Round{N: 4, Machines: []string{"M1", "M2", "M3", "M4"}, Ours: players(8), Theirs: players(11)},
		},
		"NobodyToRespond": {
			reason: "A round can't be picked against nobody.",
			round:  Round{N: 4, Machines: []string{"M1", "M2", "M3", "M4"}, Ours: players(8)},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Analyze(context.Background(), &fakeStore{}, tc.round); err == nil {
				t.Errorf("\n%s\nAnalyze(...): want an error, got none", tc.reason)
			}
		})
	}
}
