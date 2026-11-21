package cache

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/negz/mnp/internal/db"
)

func TestListTeamsFilter(t *testing.T) {
	type want struct {
		teams []db.TeamSummary
	}

	all := []db.TeamSummary{
		{Key: "CRA", Name: "Castle Crashers", Venue: "Another Castle"},
		{Key: "PYC", Name: "Pinballycule", Venue: "Ice Box"},
		{Key: "DSV", Name: "Death Savers", Venue: "Add-a-Ball"},
	}

	cases := map[string]struct {
		reason string
		search string
		want   want
	}{
		"EmptySearch": {
			reason: "An empty search should return all teams.",
			search: "",
			want:   want{teams: all},
		},
		"MatchByKey": {
			reason: "Search should match against the team key, case-insensitively.",
			search: "cra",
			want:   want{teams: []db.TeamSummary{{Key: "CRA", Name: "Castle Crashers", Venue: "Another Castle"}}},
		},
		"MatchByName": {
			reason: "Search should match against the team name, case-insensitively.",
			search: "pinballycule",
			want:   want{teams: []db.TeamSummary{{Key: "PYC", Name: "Pinballycule", Venue: "Ice Box"}}},
		},
		"MatchBySubstring": {
			reason: "Search should match substrings within key or name.",
			search: "death",
			want:   want{teams: []db.TeamSummary{{Key: "DSV", Name: "Death Savers", Venue: "Add-a-Ball"}}},
		},
		"MultipleMatches": {
			reason: "Search should return all matching teams.",
			search: "a",
			want: want{teams: []db.TeamSummary{
				{Key: "CRA", Name: "Castle Crashers", Venue: "Another Castle"},
				{Key: "PYC", Name: "Pinballycule", Venue: "Ice Box"},
				{Key: "DSV", Name: "Death Savers", Venue: "Add-a-Ball"},
			}},
		},
		"NoMatch": {
			reason: "A search that matches nothing should return nil.",
			search: "zzz",
			want:   want{teams: nil},
		},
		"CaseInsensitive": {
			reason: "Search should be case-insensitive for both key and name.",
			search: "CASTLE",
			want:   want{teams: []db.TeamSummary{{Key: "CRA", Name: "Castle Crashers", Venue: "Another Castle"}}},
		},
	}

	s := &InMemoryStore{teams: all}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.ListTeams(context.Background(), tc.search)
			if err != nil {
				t.Fatalf("\n%s\nListTeams(...): unexpected error: %v", tc.reason, err)
			}

			if diff := cmp.Diff(tc.want.teams, got); diff != "" {
				t.Errorf("\n%s\nListTeams(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestListVenuesFilter(t *testing.T) {
	type want struct {
		venues []db.Venue
	}

	all := []db.Venue{
		{ID: 1, Key: "ANC", Name: "Another Castle"},
		{ID: 2, Key: "ICB", Name: "Ice Box"},
		{ID: 3, Key: "ADB", Name: "Add-a-Ball"},
	}

	cases := map[string]struct {
		reason string
		search string
		want   want
	}{
		"EmptySearch": {
			reason: "An empty search should return all venues.",
			search: "",
			want:   want{venues: all},
		},
		"MatchByKey": {
			reason: "Search should match against the venue key, case-insensitively.",
			search: "anc",
			want:   want{venues: []db.Venue{{ID: 1, Key: "ANC", Name: "Another Castle"}}},
		},
		"MatchByName": {
			reason: "Search should match against the venue name, case-insensitively.",
			search: "ice box",
			want:   want{venues: []db.Venue{{ID: 2, Key: "ICB", Name: "Ice Box"}}},
		},
		"NoMatch": {
			reason: "A search that matches nothing should return nil.",
			search: "zzz",
			want:   want{venues: nil},
		},
	}

	s := &InMemoryStore{venues: all}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.ListVenues(context.Background(), tc.search)
			if err != nil {
				t.Fatalf("\n%s\nListVenues(...): unexpected error: %v", tc.reason, err)
			}

			if diff := cmp.Diff(tc.want.venues, got); diff != "" {
				t.Errorf("\n%s\nListVenues(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestListMachinesFilter(t *testing.T) {
	type want struct {
		machines []db.Machine
	}

	all := []db.Machine{
		{Key: "TAF", Name: "The Addams Family"},
		{Key: "MM", Name: "Medieval Madness"},
		{Key: "TZ", Name: "Twilight Zone"},
	}

	cases := map[string]struct {
		reason string
		search string
		want   want
	}{
		"EmptySearch": {
			reason: "An empty search should return all machines.",
			search: "",
			want:   want{machines: all},
		},
		"MatchByKey": {
			reason: "Search should match against the machine key, case-insensitively.",
			search: "taf",
			want:   want{machines: []db.Machine{{Key: "TAF", Name: "The Addams Family"}}},
		},
		"MatchByName": {
			reason: "Search should match against the machine name, case-insensitively.",
			search: "medieval",
			want:   want{machines: []db.Machine{{Key: "MM", Name: "Medieval Madness"}}},
		},
		"NoMatch": {
			reason: "A search that matches nothing should return nil.",
			search: "zzz",
			want:   want{machines: nil},
		},
	}

	s := &InMemoryStore{machines: all}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.ListMachines(context.Background(), tc.search)
			if err != nil {
				t.Fatalf("\n%s\nListMachines(...): unexpected error: %v", tc.reason, err)
			}

			if diff := cmp.Diff(tc.want.machines, got); diff != "" {
				t.Errorf("\n%s\nListMachines(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestListPlayersFilter(t *testing.T) {
	type want struct {
		players []db.PlayerSummary
	}

	all := []db.PlayerSummary{
		{Name: "Alice", TeamKey: "CRA", Team: "Castle Crashers"},
		{Name: "Bob", TeamKey: "PYC", Team: "Pinballycule"},
		{Name: "Charlie", TeamKey: "DSV", Team: "Death Savers"},
	}

	cases := map[string]struct {
		reason string
		search string
		want   want
	}{
		"EmptySearch": {
			reason: "An empty search should return all players.",
			search: "",
			want:   want{players: all},
		},
		"MatchByName": {
			reason: "Search should match against the player name, case-insensitively.",
			search: "alice",
			want:   want{players: []db.PlayerSummary{{Name: "Alice", TeamKey: "CRA", Team: "Castle Crashers"}}},
		},
		"MatchByTeamKey": {
			reason: "Search should match against the team key, case-insensitively.",
			search: "pyc",
			want:   want{players: []db.PlayerSummary{{Name: "Bob", TeamKey: "PYC", Team: "Pinballycule"}}},
		},
		"MatchByTeamName": {
			reason: "Search should match against the team name, case-insensitively.",
			search: "death",
			want:   want{players: []db.PlayerSummary{{Name: "Charlie", TeamKey: "DSV", Team: "Death Savers"}}},
		},
		"NoMatch": {
			reason: "A search that matches nothing should return nil.",
			search: "zzz",
			want:   want{players: nil},
		},
		"CaseInsensitive": {
			reason: "Search should be case-insensitive.",
			search: "BOB",
			want:   want{players: []db.PlayerSummary{{Name: "Bob", TeamKey: "PYC", Team: "Pinballycule"}}},
		},
	}

	s := &InMemoryStore{players: all}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.ListPlayers(context.Background(), tc.search)
			if err != nil {
				t.Fatalf("\n%s\nListPlayers(...): unexpected error: %v", tc.reason, err)
			}

			if diff := cmp.Diff(tc.want.players, got); diff != "" {
				t.Errorf("\n%s\nListPlayers(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

// statsStore is a Store with stats for one player, that counts how often
// they're loaded. If during is set, it's called while they're loading.
type statsStore struct {
	Store

	loads  int
	during func()
}

func (s *statsStore) GetSinglePlayerMachineStats(_ context.Context, name, _ string) ([]db.PlayerMachineStats, error) {
	s.loads++
	if s.during != nil {
		s.during()
	}
	if name != "Nic Cope" {
		return nil, nil
	}
	return []db.PlayerMachineStats{{MachineKey: "VEN", Games: 23}}, nil
}

func (s *statsStore) ListTeams(_ context.Context, _ string) ([]db.TeamSummary, error) {
	return nil, nil
}

func (s *statsStore) ListVenues(_ context.Context, _ string) ([]db.Venue, error) { return nil, nil }

func (s *statsStore) ListMachines(_ context.Context, _ string) ([]db.Machine, error) { return nil, nil }

func (s *statsStore) ListPlayers(_ context.Context, _ string) ([]db.PlayerSummary, error) {
	return nil, nil
}

func (s *statsStore) ListIPRs(_ context.Context) (map[string]int, error) { return nil, nil }

func (s *statsStore) GetLeagueP50(_ context.Context) (map[string]float64, error) { return nil, nil }

func (s *statsStore) GetMachineNames(_ context.Context) (map[string]string, error) { return nil, nil }

func TestGetSinglePlayerMachineStatsCache(t *testing.T) {
	cases := map[string]struct {
		reason    string
		name      string
		refresh   bool // Refresh while the first load is under way.
		wantLoads int
	}{
		"Remembered": {
			reason:    "A player's stats should be loaded once, then remembered.",
			name:      "Nic Cope",
			wantLoads: 1,
		},
		"NoStats": {
			reason:    "A name nobody's played under shouldn't be remembered.",
			name:      "Nobody",
			wantLoads: 2,
		},
		"Refreshed": {
			reason:    "Stats loaded while the cache refreshes may be stale, so shouldn't be remembered.",
			name:      "Nic Cope",
			refresh:   true,
			wantLoads: 2,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			wrapped := &statsStore{}
			s := NewInMemoryStore(wrapped)
			if tc.refresh {
				wrapped.during = func() {
					wrapped.during = nil
					if err := s.Refresh(ctx); err != nil {
						t.Fatalf("Refresh: %v", err)
					}
				}
			}
			for range 2 {
				if _, err := s.GetSinglePlayerMachineStats(ctx, tc.name, ""); err != nil {
					t.Fatalf("GetSinglePlayerMachineStats: %v", err)
				}
			}
			if wrapped.loads != tc.wantLoads {
				t.Errorf("\n%s\nGetSinglePlayerMachineStats(...) twice: want %d loads, got %d", tc.reason, tc.wantLoads, wrapped.loads)
			}
		})
	}
}
