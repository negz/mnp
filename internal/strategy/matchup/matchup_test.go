package matchup

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/negz/mnp/internal/db"
)

type MockStore struct {
	MockGetMachineNames     func(ctx context.Context) (map[string]string, error)
	MockGetTeamMachineStats func(ctx context.Context, teamKey, venueKey string) ([]db.TeamMachineStats, error)
	MockGetVenueMachines    func(ctx context.Context, venueKey string) (map[string]bool, error)
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
		venue string
		team1 string
		team2 string
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
		"BothTeamsHaveData": {
			reason: "When both teams have stats for venue machines, the result should contain matchups sorted by edge descending with correct analysis.",
			args: args{
				store: &MockStore{
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{
							"TAF": "The Addams Family",
							"MM":  "Medieval Madness",
						}, nil
					},
					MockGetVenueMachines: func(_ context.Context, _ string) (map[string]bool, error) {
						return map[string]bool{"TAF": true, "MM": true}, nil
					},
					MockGetTeamMachineStats: func(_ context.Context, teamKey, _ string) ([]db.TeamMachineStats, error) {
						stats := map[string][]db.TeamMachineStats{
							"CRA": {
								{
									MachineKey: "TAF",
									Games:      10,
									P50Score:   50_000_000,
									LikelyPlayers: []db.LikelyPlayer{
										{Name: "Alice", Games: 12, P50Score: 60_000_000},
										{Name: "Bob", Games: 8, P50Score: 40_000_000},
									},
								},
								{
									MachineKey: "MM",
									Games:      6,
									P50Score:   20_000_000,
									LikelyPlayers: []db.LikelyPlayer{
										{Name: "Alice", Games: 4, P50Score: 25_000_000},
									},
								},
							},
							"PYC": {
								{
									MachineKey: "TAF",
									Games:      8,
									P50Score:   40_000_000,
									LikelyPlayers: []db.LikelyPlayer{
										{Name: "Carol", Games: 10, P50Score: 45_000_000},
									},
								},
								{
									MachineKey: "MM",
									Games:      12,
									P50Score:   30_000_000,
									LikelyPlayers: []db.LikelyPlayer{
										{Name: "Carol", Games: 15, P50Score: 35_000_000},
									},
								},
							},
						}
						return stats[teamKey], nil
					},
				},
				venue: "SAM",
				team1: "CRA",
				team2: "PYC",
			},
			want: want{
				result: &Result{
					Venue: "SAM",
					Team1: "CRA",
					Team2: "PYC",
					Machines: []MachineMatchup{
						{
							MachineKey:  "TAF",
							MachineName: "The Addams Family",
							Team1P50:    50_000_000,
							Team1Likely: 50_000_000,
							Team2P50:    40_000_000,
							Team2Likely: 45_000_000,
							Edge:        edgePct(50_000_000, 45_000_000),
							Confidence:  ConfidenceHigh,
						},
						{
							MachineKey:  "MM",
							MachineName: "Medieval Madness",
							Team1P50:    20_000_000,
							Team1Likely: 25_000_000,
							Team2P50:    30_000_000,
							Team2Likely: 35_000_000,
							Edge:        edgePct(25_000_000, 35_000_000),
							Confidence:  ConfidenceMedium,
						},
					},
					Analysis: Analysis{
						Team1Advantages: []string{"The Addams Family"},
						Team2Advantages: []string{"Medieval Madness"},
					},
				},
			},
		},
		"OnlyTeam2HasMachine": {
			reason: "When team 2 has stats for a venue machine that team 1 has never played, it should appear with zero team 1 stats and a large negative edge.",
			args: args{
				store: &MockStore{
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{"TZ": "Twilight Zone"}, nil
					},
					MockGetVenueMachines: func(_ context.Context, _ string) (map[string]bool, error) {
						return map[string]bool{"TZ": true}, nil
					},
					MockGetTeamMachineStats: func(_ context.Context, teamKey, _ string) ([]db.TeamMachineStats, error) {
						stats := map[string][]db.TeamMachineStats{
							"PYC": {
								{
									MachineKey: "TZ",
									Games:      5,
									P50Score:   10_000_000,
									LikelyPlayers: []db.LikelyPlayer{
										{Name: "Carol", Games: 5, P50Score: 10_000_000},
									},
								},
							},
						}
						return stats[teamKey], nil
					},
				},
				venue: "SAM",
				team1: "CRA",
				team2: "PYC",
			},
			want: want{
				result: &Result{
					Venue: "SAM",
					Team1: "CRA",
					Team2: "PYC",
					Machines: []MachineMatchup{
						{
							MachineKey:  "TZ",
							MachineName: "Twilight Zone",
							Team2P50:    10_000_000,
							Team2Likely: 10_000_000,
							Edge:        -math.MaxFloat64,
							Confidence:  ConfidenceLow,
						},
					},
					Analysis: Analysis{
						Team2Advantages: []string{"Twilight Zone"},
					},
				},
			},
		},
		"MachineNotAtVenue": {
			reason: "Machines that both teams have played but that aren't at the venue should be excluded.",
			args: args{
				store: &MockStore{
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{"TAF": "The Addams Family"}, nil
					},
					MockGetVenueMachines: func(_ context.Context, _ string) (map[string]bool, error) {
						return map[string]bool{}, nil
					},
					MockGetTeamMachineStats: func(_ context.Context, _ string, _ string) ([]db.TeamMachineStats, error) {
						return []db.TeamMachineStats{
							{MachineKey: "TAF", Games: 10, P50Score: 50_000_000},
						}, nil
					},
				},
				venue: "SAM",
				team1: "CRA",
				team2: "PYC",
			},
			want: want{
				result: &Result{
					Venue:    "SAM",
					Team1:    "CRA",
					Team2:    "PYC",
					Machines: []MachineMatchup{},
				},
			},
		},
		"GetVenueMachinesError": {
			reason: "An error loading venue machines should be returned.",
			args: args{
				store: &MockStore{
					MockGetVenueMachines: func(_ context.Context, _ string) (map[string]bool, error) {
						return nil, errors.New("boom")
					},
				},
				venue: "SAM",
				team1: "CRA",
				team2: "PYC",
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
		"GetMachineNamesError": {
			reason: "An error loading machine names should be returned.",
			args: args{
				store: &MockStore{
					MockGetVenueMachines: func(_ context.Context, _ string) (map[string]bool, error) {
						return map[string]bool{}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return nil, errors.New("boom")
					},
				},
				venue: "SAM",
				team1: "CRA",
				team2: "PYC",
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
		"GetTeamMachineStatsError": {
			reason: "An error loading team stats should be returned.",
			args: args{
				store: &MockStore{
					MockGetVenueMachines: func(_ context.Context, _ string) (map[string]bool, error) {
						return map[string]bool{}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{}, nil
					},
					MockGetTeamMachineStats: func(_ context.Context, _, _ string) ([]db.TeamMachineStats, error) {
						return nil, errors.New("boom")
					},
				},
				venue: "SAM",
				team1: "CRA",
				team2: "PYC",
			},
			want: want{
				err: cmpopts.AnyError,
			},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Analyze(context.Background(), tc.args.store, tc.args.venue, tc.args.team1, tc.args.team2)

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nAnalyze(...): -want error, +got error:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.result, got); diff != "" {
				t.Errorf("\n%s\nAnalyze(...): -want result, +got result:\n%s", tc.reason, diff)
			}
		})
	}
}
