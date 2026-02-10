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
	ListVenues(ctx context.Context, search string) ([]db.Venue, error)
	ListMachines(ctx context.Context, search string) ([]db.Machine, error)
	ListSchedule(ctx context.Context, after string) ([]db.ScheduleMatch, error)
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
	leagueP50    map[string]float64
	machineNames map[string]string
}

// NewInMemoryStore returns an InMemoryStore that caches slow-changing data in
// memory.
func NewInMemoryStore(s Store) *InMemoryStore {
	return &InMemoryStore{wrapped: s}
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
	s.leagueP50 = leagueP50
	s.machineNames = machineNames

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

// Passthrough methods.

// ListSchedule passes through to the underlying store.
func (s *InMemoryStore) ListSchedule(ctx context.Context, after string) ([]db.ScheduleMatch, error) {
	return s.wrapped.ListSchedule(ctx, after)
}

// GetTeamMachineStats passes through to the underlying store.
func (s *InMemoryStore) GetTeamMachineStats(ctx context.Context, teamKey, venueKey string) ([]db.TeamMachineStats, error) {
	return s.wrapped.GetTeamMachineStats(ctx, teamKey, venueKey)
}

// GetVenueMachines passes through to the underlying store.
func (s *InMemoryStore) GetVenueMachines(ctx context.Context, venueKey string) (map[string]bool, error) {
	return s.wrapped.GetVenueMachines(ctx, venueKey)
}

// GetPlayerMachineStats passes through to the underlying store.
func (s *InMemoryStore) GetPlayerMachineStats(ctx context.Context, teamKey, machineKey, venueKey string) ([]db.PlayerStats, error) {
	return s.wrapped.GetPlayerMachineStats(ctx, teamKey, machineKey, venueKey)
}

// GetPlayerTeam passes through to the underlying store.
func (s *InMemoryStore) GetPlayerTeam(ctx context.Context, playerName string) (db.PlayerTeam, error) {
	return s.wrapped.GetPlayerTeam(ctx, playerName)
}

// GetSinglePlayerMachineStats passes through to the underlying store.
func (s *InMemoryStore) GetSinglePlayerMachineStats(ctx context.Context, playerName, venueKey string) ([]db.PlayerMachineStats, error) {
	return s.wrapped.GetSinglePlayerMachineStats(ctx, playerName, venueKey)
}
