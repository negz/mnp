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
	MockGetRosterPicks      func(ctx context.Context, teamKey string) (map[string]int, error)
	MockGetVenueMachines    func(ctx context.Context, venueKey string) (map[string]bool, error)
}

func (m *MockStore) GetMachineNames(ctx context.Context) (map[string]string, error) {
	return m.MockGetMachineNames(ctx)
}

func (m *MockStore) GetTeamMachineStats(ctx context.Context, teamKey, venueKey string) ([]db.TeamMachineStats, error) {
	return m.MockGetTeamMachineStats(ctx, teamKey, venueKey)
}

func (m *MockStore) GetRosterPicks(ctx context.Context, teamKey string) (map[string]int, error) {
	return m.MockGetRosterPicks(ctx, teamKey)
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
			reason: "When both teams have stats for venue machines, the result should contain a verdict for each machine, sorted for team 1 to pick from. Only each team's top two likely players should count toward its likely score and confidence.",
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
										{Name: "Alice", Games: 12, P50Score: 60_000_000, P90Score: 90_000_000, IPR: 4},
										{Name: "Bob", Games: 8, P50Score: 40_000_000, IPR: 2},
										{Name: "Dan", Games: 1, P50Score: 5_000_000, IPR: 1},
									},
								},
								{
									MachineKey: "MM",
									Games:      6,
									P50Score:   20_000_000,
									LikelyPlayers: []db.LikelyPlayer{
										{Name: "Alice", Games: 4, P50Score: 25_000_000, IPR: 4},
									},
								},
							},
							"PYC": {
								{
									MachineKey: "TAF",
									Games:      8,
									P50Score:   40_000_000,
									LikelyPlayers: []db.LikelyPlayer{
										{Name: "Carol", Games: 10, P50Score: 45_000_000, IPR: 3},
									},
								},
								{
									MachineKey: "MM",
									Games:      12,
									P50Score:   30_000_000,
									LikelyPlayers: []db.LikelyPlayer{
										{Name: "Carol", Games: 15, P50Score: 35_000_000, IPR: 3},
									},
								},
							},
						}
						return stats[teamKey], nil
					},
					MockGetRosterPicks: func(_ context.Context, teamKey string) (map[string]int, error) {
						picks := map[string]map[string]int{
							"CRA": {"TAF": 3, "TZ": 9},
							"PYC": {"MM": 5},
						}
						return picks[teamKey], nil
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
							Team1Likely: 50_000_000,
							Team1Players: []LikelyPlayer{
								{Name: "Alice", Games: 12, P50Score: 60_000_000, P90Score: 90_000_000, IPR: 4},
								{Name: "Bob", Games: 8, P50Score: 40_000_000, IPR: 2},
								{Name: "Dan", Games: 1, P50Score: 5_000_000, IPR: 1},
							},
							Team1Picks:  3,
							Team2Likely: 45_000_000,
							Team2Players: []LikelyPlayer{
								{Name: "Carol", Games: 10, P50Score: 45_000_000, IPR: 3},
							},
							Team1Confidence: ConfidenceHigh,
							Team2Confidence: ConfidenceHigh,
							Favorite:        "CRA",
							Verdict:         VerdictEven,
							Edge:            100.0 / 9,
						},
						{
							MachineKey:  "MM",
							MachineName: "Medieval Madness",
							Team1Likely: 25_000_000,
							Team1Players: []LikelyPlayer{
								{Name: "Alice", Games: 4, P50Score: 25_000_000, IPR: 4},
							},
							Team2Likely: 35_000_000,
							Team2Players: []LikelyPlayer{
								{Name: "Carol", Games: 15, P50Score: 35_000_000, IPR: 3},
							},
							Team2Picks:      5,
							Team1Confidence: ConfidenceMedium,
							Team2Confidence: ConfidenceHigh,
							Favorite:        "PYC",
							Verdict:         VerdictWin,
							Edge:            -40,
						},
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
										{Name: "Carol", Games: 5, P50Score: 10_000_000, IPR: 3},
									},
								},
							},
						}
						return stats[teamKey], nil
					},
					MockGetRosterPicks: func(_ context.Context, teamKey string) (map[string]int, error) {
						picks := map[string]map[string]int{
							"CRA": {"TZ": 1},
							"PYC": {"TZ": 2},
						}
						return picks[teamKey], nil
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
							Team1Picks:  1,
							Team2Likely: 10_000_000,
							Team2Players: []LikelyPlayer{
								{Name: "Carol", Games: 5, P50Score: 10_000_000, IPR: 3},
							},
							Team2Picks:      2,
							Team1Confidence: ConfidenceLow,
							Team2Confidence: ConfidenceMedium,
							Favorite:        "PYC",
							Verdict:         VerdictUnknown,
							Edge:            -math.MaxFloat64,
						},
					},
				},
			},
		},
		"NeitherTeamHasPlayed": {
			reason: "A venue machine neither team has played should still be listed, with no favorite.",
			args: args{
				store: &MockStore{
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{"TZ": "Twilight Zone"}, nil
					},
					MockGetVenueMachines: func(_ context.Context, _ string) (map[string]bool, error) {
						return map[string]bool{"TZ": true}, nil
					},
					MockGetTeamMachineStats: func(_ context.Context, _, _ string) ([]db.TeamMachineStats, error) {
						return nil, nil
					},
					MockGetRosterPicks: func(_ context.Context, _ string) (map[string]int, error) {
						return nil, nil
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
						{MachineKey: "TZ", MachineName: "Twilight Zone", Verdict: VerdictUnknown},
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
					MockGetRosterPicks: func(_ context.Context, _ string) (map[string]int, error) {
						return map[string]int{"TAF": 4}, nil
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
		"GetRosterPicksError": {
			reason: "An error loading team picks should be returned.",
			args: args{
				store: &MockStore{
					MockGetVenueMachines: func(_ context.Context, _ string) (map[string]bool, error) {
						return map[string]bool{}, nil
					},
					MockGetMachineNames: func(_ context.Context) (map[string]string, error) {
						return map[string]string{}, nil
					},
					MockGetTeamMachineStats: func(_ context.Context, _, _ string) ([]db.TeamMachineStats, error) {
						return nil, nil
					},
					MockGetRosterPicks: func(_ context.Context, _ string) (map[string]int, error) {
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

func TestCall(t *testing.T) {
	type args struct {
		l1, l2 float64
		c1, c2 Confidence
	}
	type want struct {
		favorite string
		verdict  Verdict
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Team1Double": {
			reason: "A team whose likely score is at least double the other's should be likely to double.",
			args:   args{l1: 20, l2: 10, c1: ConfidenceHigh, c2: ConfidenceMedium},
			want:   want{favorite: "CRA", verdict: VerdictDouble},
		},
		"Team2Win": {
			reason: "A team whose likely score is at least 1.2 times the other's should be likely to win.",
			args:   args{l1: 10, l2: 12, c1: ConfidenceMedium, c2: ConfidenceHigh},
			want:   want{favorite: "PYC", verdict: VerdictWin},
		},
		"Even": {
			reason: "Teams whose likely scores are within 1.2 times of each other should be even.",
			args:   args{l1: 11, l2: 10, c1: ConfidenceHigh, c2: ConfidenceHigh},
			want:   want{favorite: "CRA", verdict: VerdictEven},
		},
		"Tie": {
			reason: "Team 1 should be the favorite when likely scores are tied.",
			args:   args{l1: 10, l2: 10, c1: ConfidenceHigh, c2: ConfidenceHigh},
			want:   want{favorite: "CRA", verdict: VerdictEven},
		},
		"NeitherPlayed": {
			reason: "There should be no favorite when neither team has played the machine.",
			args:   args{},
			want:   want{favorite: "", verdict: VerdictUnknown},
		},
		"LowConfidence": {
			reason: "A big edge should be unknown when either team has low confidence.",
			args:   args{l1: 10, l2: 30, c1: ConfidenceHigh, c2: ConfidenceLow},
			want:   want{favorite: "PYC", verdict: VerdictUnknown},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			favorite, verdict := call("CRA", "PYC", tc.args.l1, tc.args.l2, tc.args.c1, tc.args.c2)
			if diff := cmp.Diff(tc.want, want{favorite: favorite, verdict: verdict}, cmp.AllowUnexported(want{})); diff != "" {
				t.Errorf("\n%s\ncall(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestSortForPicking(t *testing.T) {
	machines := []MachineMatchup{
		{MachineName: "Win", Favorite: "CRA", Verdict: VerdictWin, Edge: 50},
		{MachineName: "Loss", Favorite: "PYC", Verdict: VerdictWin, Edge: -50},
		{MachineName: "DoubleLoss", Favorite: "PYC", Verdict: VerdictDouble, Edge: -150},
		{MachineName: "UnknownLoss", Favorite: "PYC", Verdict: VerdictUnknown, Edge: -200},
		{MachineName: "EvenBehind", Favorite: "PYC", Verdict: VerdictEven, Edge: -5},
		{MachineName: "UnknownWin", Favorite: "CRA", Verdict: VerdictUnknown, Edge: 300},
		{MachineName: "Double", Favorite: "CRA", Verdict: VerdictDouble, Edge: 120},
		{MachineName: "EvenAhead", Favorite: "CRA", Verdict: VerdictEven, Edge: 5},
		{MachineName: "UnknownOnly", Favorite: "CRA", Verdict: VerdictUnknown, Edge: math.MaxFloat64},
		{MachineName: "Unplayed", Verdict: VerdictUnknown},
	}

	sortForPicking(machines, "CRA")

	got := make([]string, len(machines))
	for i, m := range machines {
		got[i] = m.MachineName
	}
	want := []string{
		"Double", "Win",
		"UnknownOnly", "UnknownWin",
		"EvenAhead", "EvenBehind",
		"Unplayed",
		"UnknownLoss",
		"Loss", "DoubleLoss",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("sortForPicking(...): -want, +got:\n%s", diff)
	}
}
