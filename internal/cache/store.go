package cache

import (
	"context"
	"strings"
	"sync"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/strategy/matchup"
	"github.com/negz/mnp/internal/strategy/player"
	"github.com/negz/mnp/internal/strategy/recommend"
	"github.com/negz/mnp/internal/strategy/scout"
)

// Store is the set of queries needed by the web UI. It composes the strategy
// package store interfaces with the list queries used to populate dropdowns.
type Store interface { //nolint:interfacebloat // Composes four strategy store interfaces plus list queries.
	scout.Store
	matchup.Store
	recommend.Store
	player.Store

	ListTeams(ctx context.Context, search string) ([]db.TeamSummary, error)
	ListPlayers(ctx context.Context, search string) ([]db.PlayerSummary, error)
	ListVenues(ctx context.Context, search string) ([]db.Venue, error)
	ListMachines(ctx context.Context, search string) ([]db.Machine, error)
	ListSchedule(ctx context.Context, after string) ([]db.ScheduleMatch, error)
	MaxSeasonNumber(ctx context.Context) (int, error)
	GetRelativeScore(ctx context.Context, playerName string, league map[string]float64) (db.RelativeScore, error)
	ListSubs(ctx context.Context, teamKey string) ([]db.PlayerSummary, error)
	ListIPRs(ctx context.Context) (map[string]int, error)
}

// An InMemoryStore wraps a Store, caching data that only changes when a sync
// runs. Cached methods serve from memory. All other methods pass through to
// the underlying store. Call Refresh after each sync to repopulate the cache.
type InMemoryStore struct {
	wrapped Store

	mu           sync.RWMutex // Protects everything below.
	teams        []db.TeamSummary
	venues       []db.Venue
	machines     []db.Machine
	players      []db.PlayerSummary
	iprs         map[string]int
	leagueP50    map[string]float64
	machineNames map[string]string

	// Suggestions load every player's stats in both lineups, so stats are
	// remembered as they're loaded, until the next refresh.
	playerStats map[string][]db.PlayerMachineStats
	relative    map[string]db.RelativeScore
	refreshes   int // How many times the cache has been refreshed.
}

// NewInMemoryStore returns an InMemoryStore that caches slow-changing data in
// memory.
func NewInMemoryStore(s Store) *InMemoryStore {
	return &InMemoryStore{
		wrapped:     s,
		playerStats: make(map[string][]db.PlayerMachineStats),
		relative:    make(map[string]db.RelativeScore),
	}
}

// Refresh repopulates the in-memory cache from the underlying store.
func (s *InMemoryStore) Refresh(ctx context.Context) error {
	teams, err := s.wrapped.ListTeams(ctx, "")
	if err != nil {
		return err
	}

	venues, err := s.wrapped.ListVenues(ctx, "")
	if err != nil {
		return err
	}

	machines, err := s.wrapped.ListMachines(ctx, "")
	if err != nil {
		return err
	}

	players, err := s.wrapped.ListPlayers(ctx, "")
	if err != nil {
		return err
	}

	iprs, err := s.wrapped.ListIPRs(ctx)
	if err != nil {
		return err
	}

	leagueP50, err := s.wrapped.GetLeagueP50(ctx)
	if err != nil {
		return err
	}

	machineNames, err := s.wrapped.GetMachineNames(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.teams = teams
	s.venues = venues
	s.machines = machines
	s.players = players
	s.iprs = iprs
	s.leagueP50 = leagueP50
	s.machineNames = machineNames
	s.playerStats = make(map[string][]db.PlayerMachineStats)
	s.relative = make(map[string]db.RelativeScore)
	s.refreshes++

	return nil
}

// Cached methods.

// ListTeams returns teams from the cache, optionally filtered by search term.
func (s *InMemoryStore) ListTeams(_ context.Context, search string) ([]db.TeamSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if search == "" {
		return s.teams, nil
	}

	search = strings.ToLower(search)
	var out []db.TeamSummary
	for _, t := range s.teams {
		if strings.Contains(strings.ToLower(t.Key), search) || strings.Contains(strings.ToLower(t.Name), search) {
			out = append(out, t)
		}
	}
	return out, nil
}

// ListVenues returns venues from the cache, optionally filtered by search term.
func (s *InMemoryStore) ListVenues(_ context.Context, search string) ([]db.Venue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if search == "" {
		return s.venues, nil
	}

	search = strings.ToLower(search)
	var out []db.Venue
	for _, v := range s.venues {
		if strings.Contains(strings.ToLower(v.Key), search) || strings.Contains(strings.ToLower(v.Name), search) {
			out = append(out, v)
		}
	}
	return out, nil
}

// ListMachines returns machines from the cache, optionally filtered by search term.
func (s *InMemoryStore) ListMachines(_ context.Context, search string) ([]db.Machine, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if search == "" {
		return s.machines, nil
	}

	search = strings.ToLower(search)
	var out []db.Machine
	for _, m := range s.machines {
		if strings.Contains(strings.ToLower(m.Key), search) || strings.Contains(strings.ToLower(m.Name), search) {
			out = append(out, m)
		}
	}
	return out, nil
}

// ListPlayers returns players from the cache, optionally filtered by search term.
func (s *InMemoryStore) ListPlayers(_ context.Context, search string) ([]db.PlayerSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if search == "" {
		return s.players, nil
	}

	search = strings.ToLower(search)
	var out []db.PlayerSummary
	for _, p := range s.players {
		if strings.Contains(strings.ToLower(p.Name), search) || strings.Contains(strings.ToLower(p.TeamKey), search) || strings.Contains(strings.ToLower(p.Team), search) {
			out = append(out, p)
		}
	}
	return out, nil
}

// GetLeagueP50 returns league-wide P50 scores from the cache.
func (s *InMemoryStore) GetLeagueP50(_ context.Context) (map[string]float64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.leagueP50, nil
}

// GetMachineNames returns machine key-to-name mappings from the cache.
func (s *InMemoryStore) GetMachineNames(_ context.Context) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.machineNames, nil
}

// ListIPRs returns every rated player's IPR from the cache.
func (s *InMemoryStore) ListIPRs(_ context.Context) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.iprs, nil
}

// GetSinglePlayerMachineStats returns a player's stats on every machine from
// the cache, loading them from the underlying store if they aren't cached.
// Stats for a single venue aren't cached.
func (s *InMemoryStore) GetSinglePlayerMachineStats(ctx context.Context, playerName, venueKey string) ([]db.PlayerMachineStats, error) {
	if venueKey != "" {
		return s.wrapped.GetSinglePlayerMachineStats(ctx, playerName, venueKey)
	}
	s.mu.RLock()
	stats, ok := s.playerStats[playerName]
	refreshes := s.refreshes
	s.mu.RUnlock()
	if ok {
		return stats, nil
	}
	stats, err := s.wrapped.GetSinglePlayerMachineStats(ctx, playerName, venueKey)
	if err != nil {
		return nil, err
	}
	// Stats loaded before a refresh are stale after it. Names nobody's
	// played under, e.g. typos, aren't worth remembering.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refreshes == refreshes && len(stats) > 0 {
		s.playerStats[playerName] = stats
	}
	return stats, nil
}

// GetRelativeScore returns how a player scores relative to the league from the
// cache, loading it from the underlying store if it isn't cached. It assumes
// league is the league's typical scores from this store, as GetLeagueP50
// returns them.
func (s *InMemoryStore) GetRelativeScore(ctx context.Context, playerName string, league map[string]float64) (db.RelativeScore, error) {
	s.mu.RLock()
	rel, ok := s.relative[playerName]
	refreshes := s.refreshes
	s.mu.RUnlock()
	if ok {
		return rel, nil
	}
	rel, err := s.wrapped.GetRelativeScore(ctx, playerName, league)
	if err != nil {
		return db.RelativeScore{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refreshes == refreshes && rel.Games > 0 {
		s.relative[playerName] = rel
	}
	return rel, nil
}

// Passthrough methods.

// MaxSeasonNumber passes through to the underlying store.
func (s *InMemoryStore) MaxSeasonNumber(ctx context.Context) (int, error) {
	return s.wrapped.MaxSeasonNumber(ctx)
}

// ListSchedule passes through to the underlying store.
func (s *InMemoryStore) ListSchedule(ctx context.Context, after string) ([]db.ScheduleMatch, error) {
	return s.wrapped.ListSchedule(ctx, after)
}

// GetTeamMachineStats passes through to the underlying store.
func (s *InMemoryStore) GetTeamMachineStats(ctx context.Context, teamKey, venueKey string) ([]db.TeamMachineStats, error) {
	return s.wrapped.GetTeamMachineStats(ctx, teamKey, venueKey)
}

// GetRosterPicks passes through to the underlying store.
func (s *InMemoryStore) GetRosterPicks(ctx context.Context, teamKey string) (map[string]int, error) {
	return s.wrapped.GetRosterPicks(ctx, teamKey)
}

// GetVenueMachines passes through to the underlying store.
func (s *InMemoryStore) GetVenueMachines(ctx context.Context, venueKey string) (map[string]bool, error) {
	return s.wrapped.GetVenueMachines(ctx, venueKey)
}

// GetPlayerMachineStats passes through to the underlying store.
func (s *InMemoryStore) GetPlayerMachineStats(ctx context.Context, teamKey, machineKey, venueKey string) ([]db.PlayerStats, error) {
	return s.wrapped.GetPlayerMachineStats(ctx, teamKey, machineKey, venueKey)
}

// GetPlayer passes through to the underlying store.
func (s *InMemoryStore) GetPlayer(ctx context.Context, playerName string) (db.PlayerSummary, error) {
	return s.wrapped.GetPlayer(ctx, playerName)
}

// ListSubs passes through to the underlying store.
func (s *InMemoryStore) ListSubs(ctx context.Context, teamKey string) ([]db.PlayerSummary, error) {
	return s.wrapped.ListSubs(ctx, teamKey)
}
