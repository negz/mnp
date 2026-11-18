package db

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// testFixture holds IDs from fixture setup for use in test assertions.
type testFixture struct {
	seasonID int64
	stnID    int64 // STN (Seattle Tavern and Pool Hall)
	gpaID    int64 // GPA (Georgetown Pizza and Arcade)
	tttID    int64 // TTT (The Trailer Trashers)
	knrID    int64 // KNR (Knight Riders)
	matchID  int64
	match2ID int64
}

// newTestStore returns an initialized in-memory SQLiteStore seeded with a
// small league fixture:
//
//	Season 23
//	Venue STN (Seattle Tavern and Pool Hall) with machines TAF, TZ
//	Venue GPA (Georgetown Pizza and Arcade) with machines MM, TZ
//	Team TTT (The Trailer Trashers) at STN — players Alice, Bob
//	Team KNR (Knight Riders) at GPA — players Carol, Dave
//	IPRs: Alice 4, Bob 2
//	One match (TTT home vs KNR away, week 1 at STN) with games and results.
//	The picking team plays as players 1 and 3, so KNR picks games 1 and 3:
//	  Game 1: TAF (doubles) — Alice 500, Bob 400, Carol 300, Dave 200
//	  Game 2: TZ  (singles) — Alice 100, Carol 150
//	  Game 3: TAF (singles) — Bob 350, Dave 250
//	  Game 4: MM  (singles) — Alice 600, Carol 700
//
// This gives us predictable P50/P90 values for each team/machine/player
// combination.
func newTestStore(t *testing.T) (*SQLiteStore, testFixture) {
	t.Helper()

	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	if err := s.Init(ctx); err != nil {
		t.Fatalf("Init: %v", err)
	}

	var f testFixture

	// Season.
	f.seasonID, err = s.UpsertSeason(ctx, 23)
	if err != nil {
		t.Fatalf("UpsertSeason: %v", err)
	}

	// Venues.
	f.stnID, err = s.UpsertVenue(ctx, "STN", "Seattle Tavern and Pool Hall")
	if err != nil {
		t.Fatalf("UpsertVenue STN: %v", err)
	}
	f.gpaID, err = s.UpsertVenue(ctx, "GPA", "Georgetown Pizza and Arcade")
	if err != nil {
		t.Fatalf("UpsertVenue GPA: %v", err)
	}

	// Machines.
	for _, m := range []Machine{
		{Key: "TAF", Name: "The Addams Family"},
		{Key: "TZ", Name: "Twilight Zone"},
		{Key: "MM", Name: "Medieval Madness"},
	} {
		if err := s.UpsertMachine(ctx, m); err != nil {
			t.Fatalf("UpsertMachine %s: %v", m.Key, err)
		}
	}

	// Venue machines: TAF and TZ at STN, MM and TZ at GPA.
	for _, vm := range []struct {
		venueID    int64
		machineKey string
	}{
		{f.stnID, "TAF"},
		{f.stnID, "TZ"},
		{f.gpaID, "MM"},
		{f.gpaID, "TZ"},
	} {
		if err := s.UpsertVenueMachine(ctx, vm.venueID, vm.machineKey); err != nil {
			t.Fatalf("UpsertVenueMachine: %v", err)
		}
	}

	// Teams.
	f.tttID, err = s.UpsertTeam(ctx, Team{Key: "TTT", Name: "The Trailer Trashers", SeasonID: f.seasonID, HomeVenueID: f.stnID})
	if err != nil {
		t.Fatalf("UpsertTeam TTT: %v", err)
	}
	f.knrID, err = s.UpsertTeam(ctx, Team{Key: "KNR", Name: "Knight Riders", SeasonID: f.seasonID, HomeVenueID: f.gpaID})
	if err != nil {
		t.Fatalf("UpsertTeam KNR: %v", err)
	}

	// Players.
	players := map[string]int64{}
	for _, name := range []string{"Alice", "Bob", "Carol", "Dave"} {
		id, err := s.UpsertPlayer(ctx, name)
		if err != nil {
			t.Fatalf("UpsertPlayer %s: %v", name, err)
		}
		players[name] = id
	}

	// Rosters: Alice & Bob on TTT, Carol & Dave on KNR.
	for _, r := range []struct {
		player string
		team   int64
	}{
		{"Alice", f.tttID},
		{"Bob", f.tttID},
		{"Carol", f.knrID},
		{"Dave", f.knrID},
	} {
		if err := s.UpsertRoster(ctx, players[r.player], r.team, "P"); err != nil {
			t.Fatalf("UpsertRoster %s: %v", r.player, err)
		}
	}

	// IPRs: Alice 4, Bob 2. Carol and Dave have none.
	for name, ipr := range map[string]int{"Alice": 4, "Bob": 2} {
		if err := s.UpsertPlayerIPR(ctx, name, ipr); err != nil {
			t.Fatalf("UpsertPlayerIPR %s: %v", name, err)
		}
	}

	// Match: TTT vs KNR, week 1 at STN.
	f.matchID, err = s.UpsertMatch(ctx, Match{
		Key:        "mnp-23-1-TTT-KNR",
		SeasonID:   f.seasonID,
		Week:       1,
		Date:       "2024-01-15",
		HomeTeamID: f.tttID,
		AwayTeamID: f.knrID,
		VenueID:    f.stnID,
	})
	if err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}

	// Games and results.
	type result struct {
		player   string
		team     int64
		position int
		score    int64
	}
	games := []struct {
		round      int
		machineKey string
		isDoubles  bool
		results    []result
	}{
		{
			round: 1, machineKey: "TAF", isDoubles: true,
			results: []result{
				{"Carol", f.knrID, 1, 300},
				{"Alice", f.tttID, 2, 500},
				{"Dave", f.knrID, 3, 200},
				{"Bob", f.tttID, 4, 400},
			},
		},
		{
			round: 2, machineKey: "TZ", isDoubles: false,
			results: []result{
				{"Alice", f.tttID, 1, 100},
				{"Carol", f.knrID, 2, 150},
			},
		},
		{
			round: 3, machineKey: "TAF", isDoubles: false,
			results: []result{
				{"Dave", f.knrID, 1, 250},
				{"Bob", f.tttID, 2, 350},
			},
		},
		{
			round: 4, machineKey: "MM", isDoubles: false,
			results: []result{
				{"Alice", f.tttID, 1, 600},
				{"Carol", f.knrID, 2, 700},
			},
		},
	}

	for _, g := range games {
		gameID, err := s.InsertGame(ctx, Game{
			MatchID:    f.matchID,
			Round:      g.round,
			MachineKey: g.machineKey,
			IsDoubles:  g.isDoubles,
		})
		if err != nil {
			t.Fatalf("InsertGame round %d: %v", g.round, err)
		}
		for _, r := range g.results {
			if err := s.InsertGameResult(ctx, GameResult{
				GameID:   gameID,
				PlayerID: players[r.player],
				TeamID:   r.team,
				Position: r.position,
				Score:    r.score,
			}); err != nil {
				t.Fatalf("InsertGameResult %s round %d: %v", r.player, g.round, err)
			}
		}
	}

	// Second match: KNR vs TTT, week 2 at GPA (no games — future match).
	f.match2ID, err = s.UpsertMatch(ctx, Match{
		Key:        "mnp-23-2-KNR-TTT",
		SeasonID:   f.seasonID,
		Week:       2,
		Date:       "2024-01-22",
		HomeTeamID: f.knrID,
		AwayTeamID: f.tttID,
		VenueID:    f.gpaID,
	})
	if err != nil {
		t.Fatalf("UpsertMatch week 2: %v", err)
	}

	// Metadata.
	if err := s.SetMetadata(ctx, "mnp_last_sync", "2024-01-15T00:00:00Z"); err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}

	return s, f
}

func TestGetTeamMachineStats(t *testing.T) {
	type args struct {
		teamKey  string
		venueKey string
	}
	type want struct {
		stats []TeamMachineStats
	}

	// TTT roster: Alice, Bob.
	// TAF scores: Alice 500, Bob 400, Bob 350 → sorted [350, 400, 500]
	//   P50: rn=(3+1)/2=2 → 400, P90: rn=(3*9+9)/10=3 → 500
	//   Per-player: Bob [350,400] P50=350 P90=400, Alice [500] P50=500 P90=500.
	//   Ranked by games desc: Bob (2), Alice (1).
	// TZ scores: Alice 100 → P50=100, P90=100. Top player: Alice.
	// MM scores: Alice 600 → P50=600, P90=600. Top player: Alice.
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"AllMachines": {
			reason: "Without venue filter, stats should include all machines the team has played.",
			args:   args{teamKey: "TTT", venueKey: ""},
			want: want{stats: []TeamMachineStats{
				{
					MachineKey: "TAF", Games: 3, P50Score: 400, P90Score: 500,
					LikelyPlayers: []LikelyPlayer{
						{Name: "Bob", Games: 2, P50Score: 350, P90Score: 400, IPR: 2},
						{Name: "Alice", Games: 1, P50Score: 500, P90Score: 500, IPR: 4},
					},
				},
				{
					MachineKey: "MM", Games: 1, P50Score: 600, P90Score: 600,
					LikelyPlayers: []LikelyPlayer{
						{Name: "Alice", Games: 1, P50Score: 600, P90Score: 600, IPR: 4},
					},
				},
				{
					MachineKey: "TZ", Games: 1, P50Score: 100, P90Score: 100,
					LikelyPlayers: []LikelyPlayer{
						{Name: "Alice", Games: 1, P50Score: 100, P90Score: 100, IPR: 4},
					},
				},
			}},
		},
		"VenueFilter": {
			reason: "With venue filter, stats should only include machines at that venue and games played there.",
			args:   args{teamKey: "TTT", venueKey: "STN"},
			want: want{stats: []TeamMachineStats{
				{
					MachineKey: "TAF", Games: 3, P50Score: 400, P90Score: 500,
					LikelyPlayers: []LikelyPlayer{
						{Name: "Bob", Games: 2, P50Score: 350, P90Score: 400, IPR: 2},
						{Name: "Alice", Games: 1, P50Score: 500, P90Score: 500, IPR: 4},
					},
				},
				{
					MachineKey: "TZ", Games: 1, P50Score: 100, P90Score: 100,
					LikelyPlayers: []LikelyPlayer{
						{Name: "Alice", Games: 1, P50Score: 100, P90Score: 100, IPR: 4},
					},
				},
			}},
		},
	}

	s, _ := newTestStore(t)
	ctx := context.Background()

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.GetTeamMachineStats(ctx, tc.args.teamKey, tc.args.venueKey)
			if err != nil {
				t.Fatalf("GetTeamMachineStats: %v", err)
			}
			if diff := cmp.Diff(tc.want.stats, got); diff != "" {
				t.Errorf("\n%s\nGetTeamMachineStats(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestGetRosterPicks(t *testing.T) {
	s, f := newTestStore(t)
	ctx := context.Background()

	// Alice picked AFM in season 21 while playing for another team.
	seasonID, err := s.UpsertSeason(ctx, 21)
	if err != nil {
		t.Fatalf("UpsertSeason: %v", err)
	}
	oldID, err := s.UpsertTeam(ctx, Team{Key: "OLD", Name: "Old Team", SeasonID: seasonID, HomeVenueID: f.stnID})
	if err != nil {
		t.Fatalf("UpsertTeam OLD: %v", err)
	}
	awayID, err := s.UpsertTeam(ctx, Team{Key: "AWY", Name: "Away Team", SeasonID: seasonID, HomeVenueID: f.gpaID})
	if err != nil {
		t.Fatalf("UpsertTeam AWY: %v", err)
	}
	matchID, err := s.UpsertMatch(ctx, Match{Key: "mnp-21-1-OLD-AWY", SeasonID: seasonID, Week: 1, HomeTeamID: oldID, AwayTeamID: awayID, VenueID: f.stnID})
	if err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}
	gameID, err := s.InsertGame(ctx, Game{MatchID: matchID, Round: 2, MachineKey: "AFM"})
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}
	alice, err := s.UpsertPlayer(ctx, "Alice")
	if err != nil {
		t.Fatalf("UpsertPlayer: %v", err)
	}
	if err := s.InsertGameResult(ctx, GameResult{GameID: gameID, PlayerID: alice, TeamID: oldID, Position: 1, Score: 100}); err != nil {
		t.Fatalf("InsertGameResult: %v", err)
	}

	// Season 23: KNR picks TAF in rounds 1 and 3, with Carol and Dave both
	// playing the round 1 doubles game. TTT picks TZ and MM in rounds 2 and 4,
	// with Alice playing both.
	cases := map[string]struct {
		reason  string
		teamKey string
		want    map[string]int
	}{
		"HomeTeam": {
			reason:  "Picks should include those the current roster made for other teams in earlier seasons.",
			teamKey: "TTT",
			want:    map[string]int{"TZ": 1, "MM": 1, "AFM": 1},
		},
		"AwayTeam": {
			reason:  "A doubles game picked by two of the roster's players should count once.",
			teamKey: "KNR",
			want:    map[string]int{"TAF": 2},
		},
		"UnknownTeam": {
			reason:  "A team with no roster should get an empty map.",
			teamKey: "NOPE",
			want:    map[string]int{},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.GetRosterPicks(ctx, tc.teamKey)
			if err != nil {
				t.Fatalf("GetRosterPicks: %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nGetRosterPicks(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestGetTeamMachineStatsRecency(t *testing.T) {
	s, f := newTestStore(t)
	ctx := context.Background()

	// Alice played TAF twice in season 15, scoring 100 both times.
	seasonID, err := s.UpsertSeason(ctx, 15)
	if err != nil {
		t.Fatalf("UpsertSeason: %v", err)
	}
	oldID, err := s.UpsertTeam(ctx, Team{Key: "OLD", Name: "Old Team", SeasonID: seasonID, HomeVenueID: f.stnID})
	if err != nil {
		t.Fatalf("UpsertTeam OLD: %v", err)
	}
	matchID, err := s.UpsertMatch(ctx, Match{Key: "mnp-15-1-OLD-OLD", SeasonID: seasonID, Week: 1, HomeTeamID: oldID, AwayTeamID: oldID, VenueID: f.stnID})
	if err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}
	alice, err := s.UpsertPlayer(ctx, "Alice")
	if err != nil {
		t.Fatalf("UpsertPlayer: %v", err)
	}
	for round := range 2 {
		gameID, err := s.InsertGame(ctx, Game{MatchID: matchID, Round: round + 2, MachineKey: "TAF"})
		if err != nil {
			t.Fatalf("InsertGame: %v", err)
		}
		if err := s.InsertGameResult(ctx, GameResult{GameID: gameID, PlayerID: alice, TeamID: oldID, Position: 1, Score: 100}); err != nil {
			t.Fatalf("InsertGameResult: %v", err)
		}
	}

	got, err := s.GetTeamMachineStats(ctx, "TTT", "STN")
	if err != nil {
		t.Fatalf("GetTeamMachineStats: %v", err)
	}

	// Season 23 TAF scores: Alice 500, Bob 400 and 350. Season 15 games count
	// a quarter as much as season 23 games toward scores, and almost nothing
	// toward who's likely to play.
	//   Team: 100 (.25), 100 (.25), 350, 400, 500 → P50 400, P90 500.
	//   Alice: 100 (.25), 100 (.25), 500 → P50 500, though she has 3 games.
	//   Bob has played TAF more lately, so he's listed first despite having
	//   fewer games.
	want := []TeamMachineStats{
		{
			MachineKey: "TAF", Games: 5, P50Score: 400, P90Score: 500,
			LikelyPlayers: []LikelyPlayer{
				{Name: "Bob", Games: 2, P50Score: 350, P90Score: 400, IPR: 2},
				{Name: "Alice", Games: 3, P50Score: 500, P90Score: 500, IPR: 4},
			},
		},
		{
			MachineKey: "TZ", Games: 1, P50Score: 100, P90Score: 100,
			LikelyPlayers: []LikelyPlayer{
				{Name: "Alice", Games: 1, P50Score: 100, P90Score: 100, IPR: 4},
			},
		},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GetTeamMachineStats(...): -want, +got:\n%s", diff)
	}
}

func TestGetLeagueP50(t *testing.T) {
	// League P50 uses all players on any current-season roster.
	// TAF: all scores [200, 250, 300, 350, 400, 500] (6 scores)
	//   P50: rn=(6+1)/2=3 → 300
	// TZ: [100, 150] → P50: rn=(2+1)/2=1 → 100
	// MM: [600, 700] → P50: rn=(2+1)/2=1 → 600
	s, _ := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetLeagueP50(ctx)
	if err != nil {
		t.Fatalf("GetLeagueP50: %v", err)
	}

	want := map[string]float64{
		"TAF": 300,
		"TZ":  100,
		"MM":  600,
	}

	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GetLeagueP50(): -want, +got:\n%s", diff)
	}
}

func TestGetPlayerMachineStats(t *testing.T) {
	type args struct {
		teamKey    string
		machineKey string
		venueKey   string
	}
	type want struct {
		stats []PlayerStats
	}

	// TTT on TAF: Alice [500] P50=500 P90=500, Bob [350,400] P50=350 P90=400.
	// Ordered by P50 desc: Alice (500), Bob (350).
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"TeamOnMachine": {
			reason: "Should return per-player stats for a team's roster on a specific machine.",
			args:   args{teamKey: "TTT", machineKey: "TAF", venueKey: ""},
			want: want{stats: []PlayerStats{
				{Name: "Alice", Games: 1, P50Score: 500, P90Score: 500, IPR: 4},
				{Name: "Bob", Games: 2, P50Score: 350, P90Score: 400, IPR: 2},
			}},
		},
		"NoResults": {
			reason: "Should return no stats when no roster players have results on a machine.",
			args:   args{teamKey: "TTT", machineKey: "NONEXISTENT", venueKey: ""},
			want:   want{stats: []PlayerStats{}},
		},
	}

	s, _ := newTestStore(t)
	ctx := context.Background()

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.GetPlayerMachineStats(ctx, tc.args.teamKey, tc.args.machineKey, tc.args.venueKey)
			if err != nil {
				t.Fatalf("GetPlayerMachineStats: %v", err)
			}
			if diff := cmp.Diff(tc.want.stats, got); diff != "" {
				t.Errorf("\n%s\nGetPlayerMachineStats(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestGetSinglePlayerMachineStats(t *testing.T) {
	type args struct {
		playerName string
		venueKey   string
	}
	type want struct {
		stats []PlayerMachineStats
	}

	// Bob has 2 TAF games and nothing else, giving a clear order.
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"AllVenues": {
			reason: "Should return per-machine stats ordered by play count descending.",
			args:   args{playerName: "Bob", venueKey: ""},
			want: want{stats: []PlayerMachineStats{
				{MachineKey: "TAF", Games: 2, P50Score: 350, P90Score: 400},
			}},
		},
		"VenueFilter": {
			reason: "With venue filter, should only include games played at that venue.",
			args:   args{playerName: "Bob", venueKey: "STN"},
			want: want{stats: []PlayerMachineStats{
				{MachineKey: "TAF", Games: 2, P50Score: 350, P90Score: 400},
			}},
		},
	}

	s, _ := newTestStore(t)
	ctx := context.Background()

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.GetSinglePlayerMachineStats(ctx, tc.args.playerName, tc.args.venueKey)
			if err != nil {
				t.Fatalf("GetSinglePlayerMachineStats: %v", err)
			}
			if diff := cmp.Diff(tc.want.stats, got); diff != "" {
				t.Errorf("\n%s\nGetSinglePlayerMachineStats(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestGetRelativeScore(t *testing.T) {
	type args struct {
		playerName string
		league     map[string]float64
	}

	// Alice scored 500 on TAF, 100 on TZ, and 600 on MM, plus a forfeit on
	// TZ added below.
	cases := map[string]struct {
		reason string
		args   args
		want   RelativeScore
	}{
		"TwiceTheLeague": {
			reason: "Should return the median log ratio to the league's typical score, skipping machines without one and forfeits.",
			args:   args{playerName: "Alice", league: map[string]float64{"TAF": 250, "TZ": 50}},
			want:   RelativeScore{Games: 2, P50: math.Log(2)},
		},
		"NoGames": {
			reason: "A player who hasn't played shouldn't have a relative score.",
			args:   args{playerName: "Nobody", league: map[string]float64{"TAF": 250}},
			want:   RelativeScore{},
		},
	}

	s, f := newTestStore(t)
	ctx := context.Background()

	alice, err := s.UpsertPlayer(ctx, "Alice")
	if err != nil {
		t.Fatalf("UpsertPlayer: %v", err)
	}
	game, err := s.InsertGame(ctx, Game{MatchID: f.matchID, Round: 3, MachineKey: "TZ"})
	if err != nil {
		t.Fatalf("InsertGame: %v", err)
	}
	if err := s.InsertGameResult(ctx, GameResult{GameID: game, PlayerID: alice, TeamID: f.tttID, Position: 2, Score: 1}); err != nil {
		t.Fatalf("InsertGameResult: %v", err)
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.GetRelativeScore(ctx, tc.args.playerName, tc.args.league)
			if err != nil {
				t.Fatalf("GetRelativeScore: %v", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nGetRelativeScore(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestGetPlayer(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetPlayer(ctx, "Alice")
	if err != nil {
		t.Fatalf("GetPlayer: %v", err)
	}

	want := PlayerSummary{Name: "Alice", TeamKey: "TTT", Team: "The Trailer Trashers", IPR: 4}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GetPlayer(...): -want, +got:\n%s", diff)
	}
}

func TestListMachines(t *testing.T) {
	type args struct {
		search string
	}
	type want struct {
		machines []Machine
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"All": {
			reason: "Without search, should return all machines that have been played, ordered by key.",
			args:   args{search: ""},
			want: want{machines: []Machine{
				{Key: "MM", Name: "Medieval Madness"},
				{Key: "TAF", Name: "The Addams Family"},
				{Key: "TZ", Name: "Twilight Zone"},
			}},
		},
		"SearchByName": {
			reason: "Should match machines by case-insensitive name substring.",
			args:   args{search: "addams"},
			want: want{machines: []Machine{
				{Key: "TAF", Name: "The Addams Family"},
			}},
		},
		"SearchByKey": {
			reason: "Should match machines by case-insensitive key substring.",
			args:   args{search: "tz"},
			want: want{machines: []Machine{
				{Key: "TZ", Name: "Twilight Zone"},
			}},
		},
		"NoMatch": {
			reason: "Should return nil when no machines match the search.",
			args:   args{search: "xyz"},
			want:   want{machines: nil},
		},
	}

	s, _ := newTestStore(t)
	ctx := context.Background()

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.ListMachines(ctx, tc.args.search)
			if err != nil {
				t.Fatalf("ListMachines: %v", err)
			}
			if diff := cmp.Diff(tc.want.machines, got); diff != "" {
				t.Errorf("\n%s\nListMachines(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestListVenues(t *testing.T) {
	type args struct {
		search string
	}
	type want struct {
		venues []Venue
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"All": {
			reason: "Without search, should return all venues ordered by key.",
			args:   args{search: ""},
			want: want{venues: []Venue{
				{Key: "GPA", Name: "Georgetown Pizza and Arcade"},
				{Key: "STN", Name: "Seattle Tavern and Pool Hall"},
			}},
		},
		"SearchByName": {
			reason: "Should match venues by case-insensitive name substring.",
			args:   args{search: "tavern"},
			want: want{venues: []Venue{
				{Key: "STN", Name: "Seattle Tavern and Pool Hall"},
			}},
		},
		"NoMatch": {
			reason: "Should return nil when no venues match the search.",
			args:   args{search: "xyz"},
			want:   want{venues: nil},
		},
	}

	s, _ := newTestStore(t)
	ctx := context.Background()

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.ListVenues(ctx, tc.args.search)
			if err != nil {
				t.Fatalf("ListVenues: %v", err)
			}
			if diff := cmp.Diff(tc.want.venues, got); diff != "" {
				t.Errorf("\n%s\nListVenues(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestListTeams(t *testing.T) {
	type args struct {
		search string
	}
	type want struct {
		teams []TeamSummary
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"All": {
			reason: "Without search, should return all teams in the current season.",
			args:   args{search: ""},
			want: want{teams: []TeamSummary{
				{Key: "KNR", Name: "Knight Riders", Venue: "Georgetown Pizza and Arcade (GPA)"},
				{Key: "TTT", Name: "The Trailer Trashers", Venue: "Seattle Tavern and Pool Hall (STN)"},
			}},
		},
		"SearchByKey": {
			reason: "Should match teams by case-insensitive key substring.",
			args:   args{search: "ttt"},
			want: want{teams: []TeamSummary{
				{Key: "TTT", Name: "The Trailer Trashers", Venue: "Seattle Tavern and Pool Hall (STN)"},
			}},
		},
	}

	s, _ := newTestStore(t)
	ctx := context.Background()

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.ListTeams(ctx, tc.args.search)
			if err != nil {
				t.Fatalf("ListTeams: %v", err)
			}
			if diff := cmp.Diff(tc.want.teams, got); diff != "" {
				t.Errorf("\n%s\nListTeams(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestGetMetadata(t *testing.T) {
	type args struct {
		key string
	}
	type want struct {
		value string
	}
	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"Exists": {
			reason: "Should return the stored value for an existing key.",
			args:   args{key: "mnp_last_sync"},
			want:   want{value: "2024-01-15T00:00:00Z"},
		},
		"Missing": {
			reason: "Should return empty string for a missing key.",
			args:   args{key: "nonexistent"},
			want:   want{value: ""},
		},
	}

	s, _ := newTestStore(t)
	ctx := context.Background()

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.GetMetadata(ctx, tc.args.key)
			if err != nil {
				t.Fatalf("GetMetadata: %v", err)
			}
			if diff := cmp.Diff(tc.want.value, got); diff != "" {
				t.Errorf("\n%s\nGetMetadata(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestRebuild(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	// Repopulate with a single machine, standing in for the archive having
	// dropped everything else. The fixture's other machines, teams, venues and
	// players should not survive the rebuild. Metadata written inside the
	// transaction must, since sync callers record the last full sync there.
	err := s.Rebuild(ctx, func(tx *SQLiteStore) error {
		if err := tx.UpsertMachine(ctx, Machine{Key: "AFM", Name: "Attack from Mars"}); err != nil {
			return err
		}
		return tx.SetMetadata(ctx, "last_full_sync", "2024-02-01T00:00:00Z")
	})
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	// ListMachines only returns machines that have been played, so query the
	// machines table directly to see the full post-rebuild set.
	machines, err := s.GetMachineNames(ctx)
	if err != nil {
		t.Fatalf("GetMachineNames: %v", err)
	}
	if diff := cmp.Diff(map[string]string{"AFM": "Attack from Mars"}, machines); diff != "" {
		t.Errorf("GetMachineNames() after rebuild: -want, +got:\n%s", diff)
	}

	teams, err := s.ListTeams(ctx, "")
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if diff := cmp.Diff([]TeamSummary(nil), teams); diff != "" {
		t.Errorf("ListTeams() after rebuild: -want, +got:\n%s", diff)
	}

	// Metadata set inside the transaction is committed with the rebuild.
	full, err := s.GetMetadata(ctx, "last_full_sync")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if diff := cmp.Diff("2024-02-01T00:00:00Z", full); diff != "" {
		t.Errorf("GetMetadata(last_full_sync) after rebuild: -want, +got:\n%s", diff)
	}

	// Metadata written before the rebuild survives it: clearData leaves
	// sync_metadata intact.
	prior, err := s.GetMetadata(ctx, "mnp_last_sync")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if diff := cmp.Diff("2024-01-15T00:00:00Z", prior); diff != "" {
		t.Errorf("GetMetadata(mnp_last_sync) after rebuild: -want, +got:\n%s", diff)
	}
}

func TestRebuildRollback(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	// An fn that writes and then fails must leave the database exactly as it
	// was: neither the clear nor its own write may persist.
	wantErr := errors.New("boom")
	err := s.Rebuild(ctx, func(tx *SQLiteStore) error {
		if err := tx.UpsertMachine(ctx, Machine{Key: "AFM", Name: "Attack from Mars"}); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Rebuild error: want %v, got %v", wantErr, err)
	}

	machines, err := s.GetMachineNames(ctx)
	if err != nil {
		t.Fatalf("GetMachineNames: %v", err)
	}
	want := map[string]string{
		"TAF": "The Addams Family",
		"TZ":  "Twilight Zone",
		"MM":  "Medieval Madness",
	}
	if diff := cmp.Diff(want, machines); diff != "" {
		t.Errorf("GetMachineNames() after failed rebuild: -want, +got:\n%s", diff)
	}
}

func TestGetVenueMachines(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetVenueMachines(ctx, "STN")
	if err != nil {
		t.Fatalf("GetVenueMachines: %v", err)
	}

	want := map[string]bool{"TAF": true, "TZ": true}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GetVenueMachines(...): -want, +got:\n%s", diff)
	}
}

func TestGetMachineNames(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	got, err := s.GetMachineNames(ctx)
	if err != nil {
		t.Fatalf("GetMachineNames: %v", err)
	}

	want := map[string]string{
		"TAF": "The Addams Family",
		"TZ":  "Twilight Zone",
		"MM":  "Medieval Madness",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("GetMachineNames(): -want, +got:\n%s", diff)
	}
}

func TestLoadedSeasons(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	got, err := s.LoadedSeasons(ctx)
	if err != nil {
		t.Fatalf("LoadedSeasons: %v", err)
	}

	want := map[int]bool{23: true}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("LoadedSeasons(): -want, +got:\n%s", diff)
	}
}

func TestMaxSeasonNumber(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	got, err := s.MaxSeasonNumber(ctx)
	if err != nil {
		t.Fatalf("MaxSeasonNumber: %v", err)
	}

	if diff := cmp.Diff(23, got); diff != "" {
		t.Errorf("MaxSeasonNumber(): -want, +got:\n%s", diff)
	}
}

func TestListSchedule(t *testing.T) {
	// Fixture has two matches:
	//   Week 1: 2024-01-15, TTT (home) vs KNR (away) at STN
	//   Week 2: 2024-01-22, KNR (home) vs TTT (away) at GPA
	cases := map[string]struct {
		reason string
		after  string
		want   []ScheduleMatch
	}{
		"AllMatches": {
			reason: "A date before both matches should return both, ordered by week.",
			after:  "2024-01-01",
			want: []ScheduleMatch{
				{
					Week:        1,
					Date:        "2024-01-15",
					HomeTeamKey: "TTT",
					HomeTeam:    "The Trailer Trashers",
					AwayTeamKey: "KNR",
					AwayTeam:    "Knight Riders",
					VenueKey:    "STN",
					Venue:       "Seattle Tavern and Pool Hall",
				},
				{
					Week:        2,
					Date:        "2024-01-22",
					HomeTeamKey: "KNR",
					HomeTeam:    "Knight Riders",
					AwayTeamKey: "TTT",
					AwayTeam:    "The Trailer Trashers",
					VenueKey:    "GPA",
					Venue:       "Georgetown Pizza and Arcade",
				},
			},
		},
		"InclusiveDate": {
			reason: "A date equal to a match's date should include that match.",
			after:  "2024-01-22",
			want: []ScheduleMatch{
				{
					Week:        2,
					Date:        "2024-01-22",
					HomeTeamKey: "KNR",
					HomeTeam:    "Knight Riders",
					AwayTeamKey: "TTT",
					AwayTeam:    "The Trailer Trashers",
					VenueKey:    "GPA",
					Venue:       "Georgetown Pizza and Arcade",
				},
			},
		},
		"AfterBoth": {
			reason: "A date after all matches should return nil.",
			after:  "2024-02-01",
			want:   nil,
		},
		"BetweenMatches": {
			reason: "A date between the two matches should return only the later one.",
			after:  "2024-01-16",
			want: []ScheduleMatch{
				{
					Week:        2,
					Date:        "2024-01-22",
					HomeTeamKey: "KNR",
					HomeTeam:    "Knight Riders",
					AwayTeamKey: "TTT",
					AwayTeam:    "The Trailer Trashers",
					VenueKey:    "GPA",
					Venue:       "Georgetown Pizza and Arcade",
				},
			},
		},
	}

	s, _ := newTestStore(t)
	ctx := context.Background()

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.ListSchedule(ctx, tc.after)
			if err != nil {
				t.Fatalf("ListSchedule: %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nListSchedule(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
