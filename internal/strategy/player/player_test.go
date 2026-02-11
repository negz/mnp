package player

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/negz/mnp/internal/db"
)

type MockStore struct {
	MockGetLeagueP50                func(ctx context.Context) (map[string]float64, error)
	MockGetMachineNames             func(ctx context.Context) (map[string]string, error)
	MockGetPlayer                   func(ctx context.Context, playerName string) (db.PlayerSummary, error)
	MockGetSinglePlayerMachineStats func(ctx context.Context, playerName, venueKey string) ([]db.PlayerMachineStats, error)
	MockGetVenueMachines            func(ctx context.Context, venueKey string) (map[string]bool, error)
}

func (m *MockStore) GetLeagueP50(ctx context.Context) (map[string]float64, error) {
	return m.MockGetLeagueP50(ctx)
}

func (m *MockStore) GetMachineNames(ctx context.Context) (map[string]string, error) {
	return m.MockGetMachineNames(ctx)
}

func (m *MockStore) GetPlayer(ctx context.Context, playerName string) (db.PlayerSummary, error) {
	return m.MockGetPlayer(ctx, playerName)
}

func (m *MockStore) GetSinglePlayerMachineStats(ctx context.Context, playerName, venueKey string) ([]db.PlayerMachineStats, error) {
	return m.MockGetSinglePlayerMachineStats(ctx, playerName, venueKey)
}

func (m *MockStore) GetVenueMachines(ctx context.Context, venueKey string) (map[string]bool, error) {
	return m.MockGetVenueMachines(ctx, venueKey)
}

func TestAnalyze(t *testing.T) {
	type args struct {
		store Store
		name  string
		opts  []Option
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
		"GlobalWithTeam": {
			reason: "Without a venue option, the result should contain global stats, analysis, and the player's team.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{
							"TAF": 30_000_000,
							"MM":  15_000_000,
							"TZ":  40_000_000,
							"AFM": 20_000_000,
						}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{
							"TAF": "The Addams Family",
							"MM":  "Medieval Madness",
							"TZ":  "Twilight Zone",
							"AFM": "Attack From Mars",
						}, nil
					},
					MockGetSinglePlayerMachineStats: func(_ context.Context, _, _ string) ([]db.PlayerMachineStats, error) {
						return []db.PlayerMachineStats{
							{MachineKey: "TAF", Games: 10, P50Score: 60_000_000, P90Score: 80_000_000},
							{MachineKey: "MM", Games: 8, P50Score: 15_000_000, P90Score: 25_000_000},
							{MachineKey: "TZ", Games: 5, P50Score: 20_000_000, P90Score: 30_000_000},
							{MachineKey: "AFM", Games: 4, P50Score: 10_000_000, P90Score: 15_000_000},
						}, nil
					},
					MockGetPlayer: func(_ context.Context, _ string) (db.PlayerSummary, error) {
						return db.PlayerSummary{TeamKey: "CRA", Team: "Castle Crashers"}, nil
					},
				},
				name: "Alice",
			},
			want: want{
				result: &Result{
					Name: "Alice",
					Team: &Team{Key: "CRA", Name: "Castle Crashers"},
					GlobalStats: []MachineStats{
						{MachineKey: "TAF", MachineName: "The Addams Family", Games: 10, P50Score: 60_000_000, P90Score: 80_000_000, LeagueP50: 30_000_000},
						{MachineKey: "MM", MachineName: "Medieval Madness", Games: 8, P50Score: 15_000_000, P90Score: 25_000_000, LeagueP50: 15_000_000},
						{MachineKey: "TZ", MachineName: "Twilight Zone", Games: 5, P50Score: 20_000_000, P90Score: 30_000_000, LeagueP50: 40_000_000},
						{MachineKey: "AFM", MachineName: "Attack From Mars", Games: 4, P50Score: 10_000_000, P90Score: 15_000_000, LeagueP50: 20_000_000},
					},
					Analysis: Analysis{
						Strongest: []string{"The Addams Family", "Medieval Madness", "Twilight Zone"},
						Weakest:   []string{"Attack From Mars", "Twilight Zone", "Medieval Madness"},
					},
				},
			},
		},
		"GlobalTeamNotFound": {
			reason: "When the player's team can't be determined, Team should be nil but the result should otherwise be correct.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{"TAF": 30_000_000}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{"TAF": "The Addams Family"}, nil
					},
					MockGetSinglePlayerMachineStats: func(_ context.Context, _, _ string) ([]db.PlayerMachineStats, error) {
						return []db.PlayerMachineStats{
							{MachineKey: "TAF", Games: 5, P50Score: 50_000_000},
						}, nil
					},
					MockGetPlayer: func(_ context.Context, _ string) (db.PlayerSummary, error) {
						return db.PlayerSummary{}, errors.New("not found")
					},
				},
				name: "Unknown",
			},
			want: want{
				result: &Result{
					Name: "Unknown",
					GlobalStats: []MachineStats{
						{MachineKey: "TAF", MachineName: "The Addams Family", Games: 5, P50Score: 50_000_000, LeagueP50: 30_000_000},
					},
					Analysis: Analysis{
						Strongest: []string{"The Addams Family"},
					},
				},
			},
		},
		"AtVenue": {
			reason: "With a venue option, global stats should be filtered to venue machines.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{"TAF": 30_000_000, "MM": 15_000_000}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{"TAF": "The Addams Family", "MM": "Medieval Madness"}, nil
					},
					MockGetVenueMachines: func(_ context.Context, _ string) (map[string]bool, error) {
						return map[string]bool{"TAF": true, "MM": true}, nil
					},
					MockGetSinglePlayerMachineStats: func(_ context.Context, _, _ string) ([]db.PlayerMachineStats, error) {
						return []db.PlayerMachineStats{
							{MachineKey: "TAF", Games: 10, P50Score: 50_000_000},
							{MachineKey: "MM", Games: 8, P50Score: 20_000_000},
							{MachineKey: "TZ", Games: 5, P50Score: 20_000_000},
						}, nil
					},
					MockGetPlayer: func(_ context.Context, _ string) (db.PlayerSummary, error) {
						return db.PlayerSummary{TeamKey: "CRA", Team: "Castle Crashers"}, nil
					},
				},
				name: "Alice",
				opts: []Option{AtVenue("SAM")},
			},
			want: want{
				result: &Result{
					Name:  "Alice",
					Venue: "SAM",
					Team:  &Team{Key: "CRA", Name: "Castle Crashers"},
					GlobalStats: []MachineStats{
						{MachineKey: "TAF", MachineName: "The Addams Family", Games: 10, P50Score: 50_000_000, LeagueP50: 30_000_000},
						{MachineKey: "MM", MachineName: "Medieval Madness", Games: 8, P50Score: 20_000_000, LeagueP50: 15_000_000},
					},
					Analysis: Analysis{
						Strongest: []string{"The Addams Family", "Medieval Madness"},
					},
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
				name: "Alice",
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
		"GetMachineNamesError": {
			reason: "An error loading machine names should be returned.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return nil, errors.New("boom")
					},
				},
				name: "Alice",
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
		"GetSinglePlayerMachineStatsError": {
			reason: "An error loading player stats should be returned.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{}, nil
					},
					MockGetSinglePlayerMachineStats: func(_ context.Context, _, _ string) ([]db.PlayerMachineStats, error) {
						return nil, errors.New("boom")
					},
				},
				name: "Alice",
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
		"GetVenueMachinesError": {
			reason: "An error loading venue machines when analyzing at a venue should be returned.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{}, nil
					},
					MockGetSinglePlayerMachineStats: func(_ context.Context, _, _ string) ([]db.PlayerMachineStats, error) {
						return nil, nil
					},
					MockGetVenueMachines: func(_ context.Context, _ string) (map[string]bool, error) {
						return nil, errors.New("boom")
					},
				},
				name: "Alice",
				opts: []Option{AtVenue("SAM")},
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Analyze(context.Background(), tc.args.store, tc.args.name, tc.args.opts...)

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nAnalyze(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.result, got); diff != "" {
				t.Errorf("\n%s\nAnalyze(...): -want result, +got result:\n%s", tc.reason, diff)
			}
		})
	}
}
