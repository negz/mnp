package scout

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/negz/mnp/internal/db"
)

type MockStore struct {
	MockGetLeagueP50        func(ctx context.Context) (map[string]float64, error)
	MockGetMachineNames     func(ctx context.Context) (map[string]string, error)
	MockGetTeamMachineStats func(ctx context.Context, teamKey, venueKey string) ([]db.TeamMachineStats, error)
	MockGetVenueMachines    func(ctx context.Context, venueKey string) (map[string]bool, error)
}

func (m *MockStore) GetLeagueP50(ctx context.Context) (map[string]float64, error) {
	return m.MockGetLeagueP50(ctx)
}

func (m *MockStore) GetMachineNames(ctx context.Context) (map[string]string, error) {
	return m.MockGetMachineNames(ctx)
}

func (m *MockStore) GetTeamMachineStats(ctx context.Context, teamKey, venueKey string) ([]db.TeamMachineStats, error) {
	return m.MockGetTeamMachineStats(ctx, teamKey, venueKey)
}

func (m *MockStore) GetVenueMachines(ctx context.Context, venueKey string) (map[string]bool, error) {
	return m.MockGetVenueMachines(ctx, venueKey)
}

func TestAnalyze(t *testing.T) {
	type args struct {
		store Store
		team  string
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
		"GlobalStats": {
			reason: "Without a venue option, the result should contain global stats and analysis based on relative strength.",
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
					MockGetTeamMachineStats: func(_ context.Context, _, _ string) ([]db.TeamMachineStats, error) {
						return []db.TeamMachineStats{
							{MachineKey: "TAF", Games: 10, P50Score: 60_000_000, P90Score: 80_000_000},
							{MachineKey: "MM", Games: 8, P50Score: 15_000_000, P90Score: 25_000_000},
							{MachineKey: "TZ", Games: 5, P50Score: 20_000_000, P90Score: 30_000_000},
							{MachineKey: "AFM", Games: 4, P50Score: 10_000_000, P90Score: 15_000_000},
						}, nil
					},
				},
				team: "CRA",
			},
			want: want{
				result: &Result{
					Team: "CRA",
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
					MockGetTeamMachineStats: func(_ context.Context, _, _ string) ([]db.TeamMachineStats, error) {
						return []db.TeamMachineStats{
							{MachineKey: "TAF", Games: 10, P50Score: 50_000_000, P90Score: 70_000_000},
							{MachineKey: "MM", Games: 8, P50Score: 20_000_000, P90Score: 30_000_000},
							{MachineKey: "TZ", Games: 5, P50Score: 20_000_000, P90Score: 30_000_000},
						}, nil
					},
				},
				team: "CRA",
				opts: []Option{AtVenue("SAM")},
			},
			want: want{
				result: &Result{
					Team:  "CRA",
					Venue: "SAM",
					GlobalStats: []MachineStats{
						{MachineKey: "TAF", MachineName: "The Addams Family", Games: 10, P50Score: 50_000_000, P90Score: 70_000_000, LeagueP50: 30_000_000},
						{MachineKey: "MM", MachineName: "Medieval Madness", Games: 8, P50Score: 20_000_000, P90Score: 30_000_000, LeagueP50: 15_000_000},
					},
					Analysis: Analysis{
						Strongest: []string{"The Addams Family", "Medieval Madness"},
					},
				},
			},
		},
		"AnalysisMinGamesFilter": {
			reason: "Machines with fewer than 3 games should be excluded from the strongest/weakest analysis.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{"TAF": 30_000_000, "MM": 15_000_000}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{"TAF": "The Addams Family", "MM": "Medieval Madness"}, nil
					},
					MockGetTeamMachineStats: func(_ context.Context, _, _ string) ([]db.TeamMachineStats, error) {
						return []db.TeamMachineStats{
							{MachineKey: "TAF", Games: 3, P50Score: 60_000_000},
							{MachineKey: "MM", Games: 2, P50Score: 100_000_000},
						}, nil
					},
				},
				team: "CRA",
			},
			want: want{
				result: &Result{
					Team: "CRA",
					GlobalStats: []MachineStats{
						{MachineKey: "TAF", MachineName: "The Addams Family", Games: 3, P50Score: 60_000_000, LeagueP50: 30_000_000},
						{MachineKey: "MM", MachineName: "Medieval Madness", Games: 2, P50Score: 100_000_000, LeagueP50: 15_000_000},
					},
					Analysis: Analysis{
						Strongest: []string{"The Addams Family"},
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
				team: "CRA",
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
				team: "CRA",
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
		"GetTeamMachineStatsError": {
			reason: "An error loading team stats should be returned.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{}, nil
					},
					MockGetTeamMachineStats: func(_ context.Context, _, _ string) ([]db.TeamMachineStats, error) {
						return nil, errors.New("boom")
					},
				},
				team: "CRA",
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
		"GetVenueMachinesError": {
			reason: "An error loading venue machines when scouting at a venue should be returned.",
			args: args{
				store: &MockStore{
					MockGetLeagueP50: func(_ context.Context) (map[string]float64, error) {
						return map[string]float64{}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{}, nil
					},
					MockGetVenueMachines: func(_ context.Context, _ string) (map[string]bool, error) {
						return nil, errors.New("boom")
					},
				},
				team: "CRA",
				opts: []Option{AtVenue("SAM")},
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Analyze(context.Background(), tc.args.store, tc.args.team, tc.args.opts...)

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nAnalyze(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.result, got); diff != "" {
				t.Errorf("\n%s\nAnalyze(...): -want result, +got result:\n%s", tc.reason, diff)
			}
		})
	}
}
