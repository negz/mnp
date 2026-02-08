package mnp

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/negz/mnp/internal/db"
)

type MockStore struct {
	MockUpsertMachine      func(ctx context.Context, m db.Machine) error
	MockUpsertVenue        func(ctx context.Context, key, name string) (int64, error)
	MockUpsertVenueMachine func(ctx context.Context, venueID int64, machineKey string) error
	MockUpsertSeason       func(ctx context.Context, number int) (int64, error)
	MockUpsertTeam         func(ctx context.Context, t db.Team) (int64, error)
	MockUpsertPlayer       func(ctx context.Context, name string) (int64, error)
	MockUpsertRoster       func(ctx context.Context, playerID, teamID int64, role string) error
	MockUpsertMatch        func(ctx context.Context, m db.Match) (int64, error)
	MockInsertGame         func(ctx context.Context, g db.Game) (int64, error)
	MockInsertGameResult   func(ctx context.Context, r db.GameResult) error
	MockDeleteMatchGames   func(ctx context.Context, matchID int64) error
	MockGetTeamID          func(ctx context.Context, key string, seasonID int64) (int64, error)
	MockListMachineKeys    func(ctx context.Context) (map[string]bool, error)
	MockLoadedSeasons      func(ctx context.Context) (map[int]bool, error)
}

func (m *MockStore) UpsertMachine(ctx context.Context, machine db.Machine) error {
	return m.MockUpsertMachine(ctx, machine)
}

func (m *MockStore) UpsertVenue(ctx context.Context, key, name string) (int64, error) {
	return m.MockUpsertVenue(ctx, key, name)
}

func (m *MockStore) UpsertVenueMachine(ctx context.Context, venueID int64, machineKey string) error {
	return m.MockUpsertVenueMachine(ctx, venueID, machineKey)
}

func (m *MockStore) UpsertSeason(ctx context.Context, number int) (int64, error) {
	return m.MockUpsertSeason(ctx, number)
}

func (m *MockStore) UpsertTeam(ctx context.Context, t db.Team) (int64, error) {
	return m.MockUpsertTeam(ctx, t)
}

func (m *MockStore) UpsertPlayer(ctx context.Context, name string) (int64, error) {
	return m.MockUpsertPlayer(ctx, name)
}

func (m *MockStore) UpsertRoster(ctx context.Context, playerID, teamID int64, role string) error {
	return m.MockUpsertRoster(ctx, playerID, teamID, role)
}

func (m *MockStore) UpsertMatch(ctx context.Context, match db.Match) (int64, error) {
	return m.MockUpsertMatch(ctx, match)
}

func (m *MockStore) InsertGame(ctx context.Context, g db.Game) (int64, error) {
	return m.MockInsertGame(ctx, g)
}

func (m *MockStore) InsertGameResult(ctx context.Context, r db.GameResult) error {
	return m.MockInsertGameResult(ctx, r)
}

func (m *MockStore) DeleteMatchGames(ctx context.Context, matchID int64) error {
	return m.MockDeleteMatchGames(ctx, matchID)
}

func (m *MockStore) GetTeamID(ctx context.Context, key string, seasonID int64) (int64, error) {
	return m.MockGetTeamID(ctx, key, seasonID)
}

func (m *MockStore) ListMachineKeys(ctx context.Context) (map[string]bool, error) {
	return m.MockListMachineKeys(ctx)
}

func (m *MockStore) LoadedSeasons(ctx context.Context) (map[int]bool, error) {
	return m.MockLoadedSeasons(ctx)
}

func TestMachinesLoad(t *testing.T) {
	type args struct {
		machines Machines
		store    Store
	}

	type want struct {
		err error
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Success": {
			reason: "All machines should be upserted into the store with the correct keys and names.",
			args: args{
				machines: Machines{raw: map[string]machineJSON{
					"taf": {Key: "TAF", Name: "The Addams Family"},
					"mm":  {Key: "MM", Name: "Medieval Madness"},
				}},
				store: &MockStore{
					MockUpsertMachine: func(_ context.Context, got db.Machine) error {
						want := map[string]db.Machine{
							"TAF": {Key: "TAF", Name: "The Addams Family"},
							"MM":  {Key: "MM", Name: "Medieval Madness"},
						}
						w, ok := want[got.Key]
						if !ok {
							t.Errorf("UpsertMachine called with unexpected key %q", got.Key)
							return nil
						}
						if diff := cmp.Diff(w, got); diff != "" {
							t.Errorf("UpsertMachine(...): -want, +got:\n%s", diff)
						}
						return nil
					},
				},
			},
			want: want{},
		},
		"Empty": {
			reason: "Loading with no machines should succeed.",
			args: args{
				machines: Machines{},
				store: &MockStore{
					MockUpsertMachine: func(_ context.Context, _ db.Machine) error {
						return nil
					},
				},
			},
			want: want{},
		},
		"UpsertMachineError": {
			reason: "An error upserting a machine should be returned.",
			args: args{
				machines: Machines{raw: map[string]machineJSON{
					"taf": {Key: "TAF", Name: "The Addams Family"},
				}},
				store: &MockStore{
					MockUpsertMachine: func(_ context.Context, _ db.Machine) error {
						return errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.args.machines.Load(context.Background(), tc.args.store)

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nMachines.Load(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestVenuesLoad(t *testing.T) {
	type args struct {
		venues Venues
		store  Store
	}

	type want struct {
		err error
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Success": {
			reason: "Venues should be upserted with correct key and name. Only known machines should be associated.",
			args: args{
				venues: Venues{raw: map[string]venueRawJSON{
					"add": {Key: "ADD", Name: "Add-a-Ball", Machines: []string{"TAF", "UNKNOWN"}},
				}},
				store: &MockStore{
					MockListMachineKeys: func(_ context.Context) (map[string]bool, error) {
						return map[string]bool{"TAF": true}, nil
					},
					MockUpsertVenue: func(_ context.Context, key, name string) (int64, error) {
						if diff := cmp.Diff("ADD", key); diff != "" {
							t.Errorf("UpsertVenue key: -want, +got:\n%s", diff)
						}
						if diff := cmp.Diff("Add-a-Ball", name); diff != "" {
							t.Errorf("UpsertVenue name: -want, +got:\n%s", diff)
						}
						return 1, nil
					},
					MockUpsertVenueMachine: func(_ context.Context, venueID int64, machineKey string) error {
						if diff := cmp.Diff(int64(1), venueID); diff != "" {
							t.Errorf("UpsertVenueMachine venueID: -want, +got:\n%s", diff)
						}
						if diff := cmp.Diff("TAF", machineKey); diff != "" {
							t.Errorf("UpsertVenueMachine machineKey: -want, +got:\n%s", diff)
						}
						return nil
					},
				},
			},
			want: want{},
		},
		"ListMachineKeysError": {
			reason: "An error listing machine keys should be returned.",
			args: args{
				venues: Venues{raw: map[string]venueRawJSON{
					"add": {Key: "ADD", Name: "Add-a-Ball"},
				}},
				store: &MockStore{
					MockListMachineKeys: func(_ context.Context) (map[string]bool, error) {
						return nil, errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"UpsertVenueError": {
			reason: "An error upserting a venue should be returned.",
			args: args{
				venues: Venues{raw: map[string]venueRawJSON{
					"add": {Key: "ADD", Name: "Add-a-Ball"},
				}},
				store: &MockStore{
					MockListMachineKeys: func(_ context.Context) (map[string]bool, error) {
						return map[string]bool{}, nil
					},
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						return 0, errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"UpsertVenueMachineError": {
			reason: "An error upserting a venue machine should be returned.",
			args: args{
				venues: Venues{raw: map[string]venueRawJSON{
					"add": {Key: "ADD", Name: "Add-a-Ball", Machines: []string{"TAF"}},
				}},
				store: &MockStore{
					MockListMachineKeys: func(_ context.Context) (map[string]bool, error) {
						return map[string]bool{"TAF": true}, nil
					},
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						return 1, nil
					},
					MockUpsertVenueMachine: func(_ context.Context, _ int64, _ string) error {
						return errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.args.venues.Load(context.Background(), tc.args.store)

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nVenues.Load(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestSeasonLoad(t *testing.T) {
	type args struct {
		season    Season
		seasonNum int
		store     Store
	}

	type want struct {
		seasonID int64
		err      error
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Success": {
			reason: "A season with one team and one player should upsert the season, venue, team, player, and roster with the correct transformed data.",
			args: args{
				seasonNum: 25,
				season: Season{raw: seasonRawJSON{
					Teams: map[string]teamSeasonJSON{
						"cra": {
							Key:   "CRA",
							Name:  "Crazies",
							Venue: "ADD",
							Roster: []struct {
								Name string `json:"name"`
							}{
								{Name: "Alice"},
							},
						},
					},
				}},
				store: &MockStore{
					MockUpsertSeason: func(_ context.Context, num int) (int64, error) {
						if diff := cmp.Diff(25, num); diff != "" {
							t.Errorf("UpsertSeason number: -want, +got:\n%s", diff)
						}
						return 100, nil
					},
					MockUpsertVenue: func(_ context.Context, key, name string) (int64, error) {
						if diff := cmp.Diff("ADD", key); diff != "" {
							t.Errorf("UpsertVenue key: -want, +got:\n%s", diff)
						}
						if diff := cmp.Diff("ADD", name); diff != "" {
							t.Errorf("UpsertVenue name: -want, +got:\n%s", diff)
						}
						return 10, nil
					},
					MockUpsertTeam: func(_ context.Context, got db.Team) (int64, error) {
						want := db.Team{Key: "CRA", Name: "Crazies", SeasonID: 100, HomeVenueID: 10}
						if diff := cmp.Diff(want, got); diff != "" {
							t.Errorf("UpsertTeam(...): -want, +got:\n%s", diff)
						}
						return 50, nil
					},
					MockUpsertPlayer: func(_ context.Context, name string) (int64, error) {
						if diff := cmp.Diff("Alice", name); diff != "" {
							t.Errorf("UpsertPlayer name: -want, +got:\n%s", diff)
						}
						return 200, nil
					},
					MockUpsertRoster: func(_ context.Context, playerID, teamID int64, role string) error {
						if diff := cmp.Diff(int64(200), playerID); diff != "" {
							t.Errorf("UpsertRoster playerID: -want, +got:\n%s", diff)
						}
						if diff := cmp.Diff(int64(50), teamID); diff != "" {
							t.Errorf("UpsertRoster teamID: -want, +got:\n%s", diff)
						}
						if diff := cmp.Diff(RolePlayer, role); diff != "" {
							t.Errorf("UpsertRoster role: -want, +got:\n%s", diff)
						}
						return nil
					},
				},
			},
			want: want{seasonID: 100},
		},
		"NoVenue": {
			reason: "A team without a venue should not upsert a venue.",
			args: args{
				seasonNum: 25,
				season: Season{raw: seasonRawJSON{
					Teams: map[string]teamSeasonJSON{
						"cra": {Key: "CRA", Name: "Crazies"},
					},
				}},
				store: &MockStore{
					MockUpsertSeason: func(_ context.Context, _ int) (int64, error) {
						return 100, nil
					},
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						t.Fatal("UpsertVenue should not be called for team without venue")
						return 0, nil
					},
					MockUpsertTeam: func(_ context.Context, _ db.Team) (int64, error) {
						return 50, nil
					},
				},
			},
			want: want{seasonID: 100},
		},
		"UpsertSeasonError": {
			reason: "An error upserting the season should be returned.",
			args: args{
				seasonNum: 25,
				season:    Season{},
				store: &MockStore{
					MockUpsertSeason: func(_ context.Context, _ int) (int64, error) {
						return 0, errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"UpsertVenueError": {
			reason: "An error upserting a team's venue should be returned.",
			args: args{
				seasonNum: 25,
				season: Season{raw: seasonRawJSON{
					Teams: map[string]teamSeasonJSON{
						"cra": {Key: "CRA", Name: "Crazies", Venue: "ADD"},
					},
				}},
				store: &MockStore{
					MockUpsertSeason: func(_ context.Context, _ int) (int64, error) {
						return 100, nil
					},
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						return 0, errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"UpsertTeamError": {
			reason: "An error upserting a team should be returned.",
			args: args{
				seasonNum: 25,
				season: Season{raw: seasonRawJSON{
					Teams: map[string]teamSeasonJSON{
						"cra": {Key: "CRA", Name: "Crazies"},
					},
				}},
				store: &MockStore{
					MockUpsertSeason: func(_ context.Context, _ int) (int64, error) {
						return 100, nil
					},
					MockUpsertTeam: func(_ context.Context, _ db.Team) (int64, error) {
						return 0, errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"UpsertPlayerError": {
			reason: "An error upserting a player should be returned.",
			args: args{
				seasonNum: 25,
				season: Season{raw: seasonRawJSON{
					Teams: map[string]teamSeasonJSON{
						"cra": {
							Key:  "CRA",
							Name: "Crazies",
							Roster: []struct {
								Name string `json:"name"`
							}{
								{Name: "Alice"},
							},
						},
					},
				}},
				store: &MockStore{
					MockUpsertSeason: func(_ context.Context, _ int) (int64, error) {
						return 100, nil
					},
					MockUpsertTeam: func(_ context.Context, _ db.Team) (int64, error) {
						return 50, nil
					},
					MockUpsertPlayer: func(_ context.Context, _ string) (int64, error) {
						return 0, errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"UpsertRosterError": {
			reason: "An error upserting a roster entry should be returned.",
			args: args{
				seasonNum: 25,
				season: Season{raw: seasonRawJSON{
					Teams: map[string]teamSeasonJSON{
						"cra": {
							Key:  "CRA",
							Name: "Crazies",
							Roster: []struct {
								Name string `json:"name"`
							}{
								{Name: "Alice"},
							},
						},
					},
				}},
				store: &MockStore{
					MockUpsertSeason: func(_ context.Context, _ int) (int64, error) {
						return 100, nil
					},
					MockUpsertTeam: func(_ context.Context, _ db.Team) (int64, error) {
						return 50, nil
					},
					MockUpsertPlayer: func(_ context.Context, _ string) (int64, error) {
						return 200, nil
					},
					MockUpsertRoster: func(_ context.Context, _, _ int64, _ string) error {
						return errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := tc.args.season.Load(context.Background(), tc.args.store, tc.args.seasonNum)

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nSeason.Load(...): -want error, +got error:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want.seasonID, got); diff != "" {
				t.Errorf("\n%s\nSeason.Load(...): -want seasonID, +got seasonID:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestMatchLoad(t *testing.T) {
	type args struct {
		match    Match
		seasonID int64
		store    Store
	}

	type want struct {
		err error
	}

	singlesMatch := Match{raw: matchRawJSON{
		Key:   "match-1",
		Week:  "3",
		Date:  "2024-01-15",
		Venue: venueRefJSON{Key: "ADD", Name: "Add-a-Ball"},
		Home: teamMatchJSON{
			Key:    "CRA",
			Name:   "Crazies",
			Lineup: []lineupJSON{{Key: "h1", Name: "Alice"}},
		},
		Away: teamMatchJSON{
			Key:    "PIN",
			Name:   "Pinheads",
			Lineup: []lineupJSON{{Key: "a1", Name: "Bob"}},
		},
		Rounds: []roundJSON{
			{
				N: 2,
				Games: []gameJSON{
					{
						N:       1,
						Machine: "TAF",
						Done:    true,
						Player1: "a1",
						Player2: "h1",
						Score1:  50_000_000,
						Score2:  30_000_000,
					},
				},
			},
		},
	}}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Success": {
			reason: "A match with one singles game should transform and load with the correct venue, teams, match, game, and player result data.",
			args: args{
				match:    singlesMatch,
				seasonID: 100,
				store: &MockStore{
					MockUpsertVenue: func(_ context.Context, key, name string) (int64, error) {
						if diff := cmp.Diff("ADD", key); diff != "" {
							t.Errorf("UpsertVenue key: -want, +got:\n%s", diff)
						}
						if diff := cmp.Diff("Add-a-Ball", name); diff != "" {
							t.Errorf("UpsertVenue name: -want, +got:\n%s", diff)
						}
						return 10, nil
					},
					MockGetTeamID: func(_ context.Context, key string, seasonID int64) (int64, error) {
						if diff := cmp.Diff(int64(100), seasonID); diff != "" {
							t.Errorf("GetTeamID seasonID: -want, +got:\n%s", diff)
						}
						if key == "CRA" {
							return 50, nil
						}
						return 60, nil
					},
					MockUpsertMatch: func(_ context.Context, got db.Match) (int64, error) {
						want := db.Match{
							Key:        "match-1",
							SeasonID:   100,
							Week:       3,
							Date:       "2024-01-15",
							HomeTeamID: 50,
							AwayTeamID: 60,
							VenueID:    10,
						}
						if diff := cmp.Diff(want, got); diff != "" {
							t.Errorf("UpsertMatch(...): -want, +got:\n%s", diff)
						}
						return 500, nil
					},
					MockDeleteMatchGames: func(_ context.Context, matchID int64) error {
						if diff := cmp.Diff(int64(500), matchID); diff != "" {
							t.Errorf("DeleteMatchGames matchID: -want, +got:\n%s", diff)
						}
						return nil
					},
					MockInsertGame: func(_ context.Context, got db.Game) (int64, error) {
						want := db.Game{
							MatchID:    500,
							Round:      2,
							MachineKey: "TAF",
							IsDoubles:  false,
						}
						if diff := cmp.Diff(want, got); diff != "" {
							t.Errorf("InsertGame(...): -want, +got:\n%s", diff)
						}
						return 1000, nil
					},
					MockUpsertPlayer: func(_ context.Context, name string) (int64, error) {
						switch name {
						case "Bob":
							return 201, nil
						case "Alice":
							return 200, nil
						default:
							t.Errorf("UpsertPlayer called with unexpected name %q", name)
							return 0, nil
						}
					},
					MockInsertGameResult: func(_ context.Context, got db.GameResult) error {
						want := map[int]db.GameResult{
							1: {GameID: 1000, PlayerID: 201, TeamID: 60, Position: 1, Score: 50_000_000},
							2: {GameID: 1000, PlayerID: 200, TeamID: 50, Position: 2, Score: 30_000_000},
						}
						w, ok := want[got.Position]
						if !ok {
							t.Errorf("InsertGameResult called with unexpected position %d", got.Position)
							return nil
						}
						if diff := cmp.Diff(w, got); diff != "" {
							t.Errorf("InsertGameResult(...): -want, +got:\n%s", diff)
						}
						return nil
					},
				},
			},
			want: want{},
		},
		"NoVenue": {
			reason: "A match without a venue should not upsert a venue.",
			args: args{
				match: Match{raw: matchRawJSON{
					Key:  "match-1",
					Week: "1",
					Home: teamMatchJSON{Key: "CRA"},
					Away: teamMatchJSON{Key: "PIN"},
				}},
				seasonID: 100,
				store: &MockStore{
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						t.Fatal("UpsertVenue should not be called for match without venue")
						return 0, nil
					},
					MockGetTeamID: func(_ context.Context, _ string, _ int64) (int64, error) {
						return 50, nil
					},
					MockUpsertMatch: func(_ context.Context, _ db.Match) (int64, error) {
						return 500, nil
					},
					MockDeleteMatchGames: func(_ context.Context, _ int64) error {
						return nil
					},
				},
			},
			want: want{},
		},
		"UpsertVenueError": {
			reason: "An error upserting a match venue should be returned.",
			args: args{
				match:    singlesMatch,
				seasonID: 100,
				store: &MockStore{
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						return 0, errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"GetHomeTeamError": {
			reason: "An error resolving the home team should be returned.",
			args: args{
				match:    singlesMatch,
				seasonID: 100,
				store: &MockStore{
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						return 10, nil
					},
					MockGetTeamID: func(_ context.Context, key string, _ int64) (int64, error) {
						if key == "CRA" {
							return 0, errors.New("boom")
						}
						return 60, nil
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"GetAwayTeamError": {
			reason: "An error resolving the away team should be returned.",
			args: args{
				match:    singlesMatch,
				seasonID: 100,
				store: &MockStore{
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						return 10, nil
					},
					MockGetTeamID: func(_ context.Context, key string, _ int64) (int64, error) {
						if key == "PIN" {
							return 0, errors.New("boom")
						}
						return 50, nil
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"UpsertMatchError": {
			reason: "An error upserting the match should be returned.",
			args: args{
				match:    singlesMatch,
				seasonID: 100,
				store: &MockStore{
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						return 10, nil
					},
					MockGetTeamID: func(_ context.Context, _ string, _ int64) (int64, error) {
						return 50, nil
					},
					MockUpsertMatch: func(_ context.Context, _ db.Match) (int64, error) {
						return 0, errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"DeleteMatchGamesError": {
			reason: "An error deleting old match games should be returned.",
			args: args{
				match:    singlesMatch,
				seasonID: 100,
				store: &MockStore{
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						return 10, nil
					},
					MockGetTeamID: func(_ context.Context, _ string, _ int64) (int64, error) {
						return 50, nil
					},
					MockUpsertMatch: func(_ context.Context, _ db.Match) (int64, error) {
						return 500, nil
					},
					MockDeleteMatchGames: func(_ context.Context, _ int64) error {
						return errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"InsertGameError": {
			reason: "An error inserting a game should be returned.",
			args: args{
				match:    singlesMatch,
				seasonID: 100,
				store: &MockStore{
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						return 10, nil
					},
					MockGetTeamID: func(_ context.Context, _ string, _ int64) (int64, error) {
						return 50, nil
					},
					MockUpsertMatch: func(_ context.Context, _ db.Match) (int64, error) {
						return 500, nil
					},
					MockDeleteMatchGames: func(_ context.Context, _ int64) error {
						return nil
					},
					MockInsertGame: func(_ context.Context, _ db.Game) (int64, error) {
						return 0, errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"UpsertPlayerError": {
			reason: "An error upserting a player from game results should be returned.",
			args: args{
				match:    singlesMatch,
				seasonID: 100,
				store: &MockStore{
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						return 10, nil
					},
					MockGetTeamID: func(_ context.Context, _ string, _ int64) (int64, error) {
						return 50, nil
					},
					MockUpsertMatch: func(_ context.Context, _ db.Match) (int64, error) {
						return 500, nil
					},
					MockDeleteMatchGames: func(_ context.Context, _ int64) error {
						return nil
					},
					MockInsertGame: func(_ context.Context, _ db.Game) (int64, error) {
						return 1000, nil
					},
					MockUpsertPlayer: func(_ context.Context, _ string) (int64, error) {
						return 0, errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
		"InsertGameResultError": {
			reason: "An error inserting a game result should be returned.",
			args: args{
				match:    singlesMatch,
				seasonID: 100,
				store: &MockStore{
					MockUpsertVenue: func(_ context.Context, _, _ string) (int64, error) {
						return 10, nil
					},
					MockGetTeamID: func(_ context.Context, _ string, _ int64) (int64, error) {
						return 50, nil
					},
					MockUpsertMatch: func(_ context.Context, _ db.Match) (int64, error) {
						return 500, nil
					},
					MockDeleteMatchGames: func(_ context.Context, _ int64) error {
						return nil
					},
					MockInsertGame: func(_ context.Context, _ db.Game) (int64, error) {
						return 1000, nil
					},
					MockUpsertPlayer: func(_ context.Context, _ string) (int64, error) {
						return 200, nil
					},
					MockInsertGameResult: func(_ context.Context, _ db.GameResult) error {
						return errors.New("boom")
					},
				},
			},
			want: want{err: cmpopts.AnyError},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.args.match.Load(context.Background(), tc.args.store, tc.args.seasonID)

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nMatch.Load(...): -want error, +got error:\n%s", tc.reason, diff)
			}
		})
	}
}
