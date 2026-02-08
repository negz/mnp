package recommend

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/negz/mnp/internal/db"
)

type MockStore struct {
	MockGetLeagueP50          func(ctx context.Context) (map[string]float64, error)
	MockGetPlayerMachineStats func(ctx context.Context, teamKey, machineKey, venueKey string) ([]db.PlayerStats, error)
}

func (m *MockStore) GetLeagueP50(ctx context.Context) (map[string]float64, error) {
	return m.MockGetLeagueP50(ctx)
}

func (m *MockStore) GetPlayerMachineStats(ctx context.Context, teamKey, machineKey, venueKey string) ([]db.PlayerStats, error) {
	return m.MockGetPlayerMachineStats(ctx, teamKey, machineKey, venueKey)
}

func TestAnalyze(t *testing.T) {
	type args struct {
		store   Store
		team    string
		machine string
		opts    []Option
	}

	type want struct {
		result *Result
		err    error
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"GlobalStats": {
			reason: "Without options, the result should contain global player stats enriched with league P50.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{"TAF": 30_000_000}, nil
					},
					MockGetPlayerMachineStats: func(_ context.Context, _, _, _ string) ([]db.PlayerStats, error) {
						return []db.PlayerStats{
							{Name: "Alice", Games: 10, P50Score: 50_000_000, P90Score: 70_000_000},
							{Name: "Bob", Games: 5, P50Score: 30_000_000, P90Score: 40_000_000},
						}, nil
					},
				},
				team:    "CRA",
				machine: "TAF",
			},
			want: want{
				result: &Result{
					Team:    "CRA",
					Machine: "TAF",
					GlobalStats: []PlayerStats{
						{Name: "Alice", Games: 10, P50Score: 50_000_000, P90Score: 70_000_000, LeagueP50: 30_000_000},
						{Name: "Bob", Games: 5, P50Score: 30_000_000, P90Score: 40_000_000, LeagueP50: 30_000_000},
					},
				},
			},
		},
		"AtVenue": {
			reason: "With a venue option, venue stats should be populated and global stats should flag players missing venue-specific data.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{"TAF": 30_000_000}, nil
					},
					MockGetPlayerMachineStats: func(_ context.Context, _, _, venueKey string) ([]db.PlayerStats, error) {
						if venueKey != "" {
							return []db.PlayerStats{
								{Name: "Alice", Games: 3, P50Score: 45_000_000, P90Score: 55_000_000},
							}, nil
						}
						return []db.PlayerStats{
							{Name: "Alice", Games: 10, P50Score: 50_000_000, P90Score: 70_000_000},
							{Name: "Bob", Games: 5, P50Score: 30_000_000, P90Score: 40_000_000},
						}, nil
					},
				},
				team:    "CRA",
				machine: "TAF",
				opts:    []Option{AtVenue("SAM")},
			},
			want: want{
				result: &Result{
					Team:    "CRA",
					Machine: "TAF",
					Venue:   "SAM",
					VenueStats: []PlayerStats{
						{Name: "Alice", Games: 3, P50Score: 45_000_000, P90Score: 55_000_000, LeagueP50: 30_000_000},
					},
					GlobalStats: []PlayerStats{
						{Name: "Alice", Games: 10, P50Score: 50_000_000, P90Score: 70_000_000, LeagueP50: 30_000_000},
						{Name: "Bob", Games: 5, P50Score: 30_000_000, P90Score: 40_000_000, LeagueP50: 30_000_000, NoVenueData: true},
					},
				},
			},
		},
		"VsOpponentStrong": {
			reason: "When our best player outscores theirs by more than 1M, the verdict should be Strong.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{"TAF": 30_000_000}, nil
					},
					MockGetPlayerMachineStats: func(_ context.Context, teamKey, _, _ string) ([]db.PlayerStats, error) {
						stats := map[string][]db.PlayerStats{
							"CRA": {{Name: "Alice", Games: 10, P50Score: 50_000_000}},
							"PYC": {{Name: "Carol", Games: 8, P50Score: 40_000_000}},
						}
						return stats[teamKey], nil
					},
				},
				team:    "CRA",
				machine: "TAF",
				opts:    []Option{VsOpponent("PYC")},
			},
			want: want{
				result: &Result{
					Team:          "CRA",
					Machine:       "TAF",
					Opponent:      "PYC",
					GlobalStats:   []PlayerStats{{Name: "Alice", Games: 10, P50Score: 50_000_000, LeagueP50: 30_000_000}},
					OpponentStats: []PlayerStats{{Name: "Carol", Games: 8, P50Score: 40_000_000, LeagueP50: 30_000_000}},
					Assessment: &Assessment{
						OurBest:   "Alice",
						TheirBest: "Carol",
						Diff:      10_000_000,
						Verdict:   VerdictStrong,
					},
				},
			},
		},
		"VsOpponentWeak": {
			reason: "When their best player outscores ours by more than 1M, the verdict should be Weak.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{"TAF": 30_000_000}, nil
					},
					MockGetPlayerMachineStats: func(_ context.Context, teamKey, _, _ string) ([]db.PlayerStats, error) {
						stats := map[string][]db.PlayerStats{
							"CRA": {{Name: "Alice", Games: 10, P50Score: 30_000_000}},
							"PYC": {{Name: "Carol", Games: 8, P50Score: 50_000_000}},
						}
						return stats[teamKey], nil
					},
				},
				team:    "CRA",
				machine: "TAF",
				opts:    []Option{VsOpponent("PYC")},
			},
			want: want{
				result: &Result{
					Team:          "CRA",
					Machine:       "TAF",
					Opponent:      "PYC",
					GlobalStats:   []PlayerStats{{Name: "Alice", Games: 10, P50Score: 30_000_000, LeagueP50: 30_000_000}},
					OpponentStats: []PlayerStats{{Name: "Carol", Games: 8, P50Score: 50_000_000, LeagueP50: 30_000_000}},
					Assessment: &Assessment{
						OurBest:   "Alice",
						TheirBest: "Carol",
						Diff:      -20_000_000,
						Verdict:   VerdictWeak,
					},
				},
			},
		},
		"VsOpponentContested": {
			reason: "When the best players are within 1M of each other, the verdict should be Contested.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{"TAF": 30_000_000}, nil
					},
					MockGetPlayerMachineStats: func(_ context.Context, teamKey, _, _ string) ([]db.PlayerStats, error) {
						stats := map[string][]db.PlayerStats{
							"CRA": {{Name: "Alice", Games: 10, P50Score: 30_500_000}},
							"PYC": {{Name: "Carol", Games: 8, P50Score: 30_000_000}},
						}
						return stats[teamKey], nil
					},
				},
				team:    "CRA",
				machine: "TAF",
				opts:    []Option{VsOpponent("PYC")},
			},
			want: want{
				result: &Result{
					Team:          "CRA",
					Machine:       "TAF",
					Opponent:      "PYC",
					GlobalStats:   []PlayerStats{{Name: "Alice", Games: 10, P50Score: 30_500_000, LeagueP50: 30_000_000}},
					OpponentStats: []PlayerStats{{Name: "Carol", Games: 8, P50Score: 30_000_000, LeagueP50: 30_000_000}},
					Assessment: &Assessment{
						OurBest:   "Alice",
						TheirBest: "Carol",
						Diff:      500_000,
						Verdict:   VerdictContested,
					},
				},
			},
		},
		"VsOpponentNoAssessment": {
			reason: "When the opponent has no players, there should be no assessment.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{"TAF": 30_000_000}, nil
					},
					MockGetPlayerMachineStats: func(_ context.Context, teamKey, _, _ string) ([]db.PlayerStats, error) {
						stats := map[string][]db.PlayerStats{
							"CRA": {{Name: "Alice", Games: 10, P50Score: 50_000_000}},
						}
						return stats[teamKey], nil
					},
				},
				team:    "CRA",
				machine: "TAF",
				opts:    []Option{VsOpponent("PYC")},
			},
			want: want{
				result: &Result{
					Team:          "CRA",
					Machine:       "TAF",
					Opponent:      "PYC",
					GlobalStats:   []PlayerStats{{Name: "Alice", Games: 10, P50Score: 50_000_000, LeagueP50: 30_000_000}},
					OpponentStats: []PlayerStats{},
				},
			},
		},
		"GetLeagueP50Error": {
			reason: "An error loading league P50 should be returned.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return nil, errors.New("boom")
					},
				},
				team:    "CRA",
				machine: "TAF",
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
		"GetPlayerMachineStatsError": {
			reason: "An error loading player stats should be returned.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{}, nil
					},
					MockGetPlayerMachineStats: func(_ context.Context, _, _, _ string) ([]db.PlayerStats, error) {
						return nil, errors.New("boom")
					},
				},
				team:    "CRA",
				machine: "TAF",
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Analyze(context.Background(), tc.args.store, tc.args.team, tc.args.machine, tc.args.opts...)

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nAnalyze(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.result, got); diff != "" {
				t.Errorf("\n%s\nAnalyze(...): -want result, +got result:\n%s", tc.reason, diff)
			}
		})
	}
}
