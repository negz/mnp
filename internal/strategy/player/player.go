// Package player provides individual player analysis across machines.
package player

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/negz/mnp/internal/db"
)

const minGamesForAnalysis = 3

// Store is the set of queries needed for player analysis.
type Store interface {
	GetLeagueP50(ctx context.Context) (map[string]float64, error)
	GetMachineNames(ctx context.Context) (map[string]string, error)
	GetPlayerTeam(ctx context.Context, playerName string) (db.PlayerTeam, error)
	GetSinglePlayerMachineStats(ctx context.Context, playerName, venueKey string) ([]db.PlayerMachineStats, error)
	GetVenueMachines(ctx context.Context, venueKey string) (map[string]bool, error)
}

// MachineStats is a player's performance on a single machine.
type MachineStats struct {
	MachineKey  string
	MachineName string
	Games       int
	P50Score    float64
	P90Score    float64
	LeagueP50   float64
	NoVenueData bool // True in global stats when this machine has no venue-specific data.
}

// Team is the player's current team.
type Team struct {
	Key  string
	Name string
}

// Analysis summarizes a player's strongest and weakest machines.
type Analysis struct {
	Strongest []string // Machine names, up to 3.
	Weakest   []string // Machine names, up to 3.
}

// Result is the output of a Player query.
type Result struct {
	Name        string
	Venue       string         // Empty for global-only queries.
	Team        *Team          // Nil if player's team can't be determined.
	VenueStats  []MachineStats // Nil if no venue requested or no venue data.
	GlobalStats []MachineStats // All machines (no venue) or venue machines with global data.
	Analysis    Analysis
}

// Option configures a Player query.
type Option func(*Options)

// Options holds optional parameters for a Player query.
type Options struct {
	venue string
}

// AtVenue filters player stats to a specific venue.
func AtVenue(key string) Option {
	return func(o *Options) {
		o.venue = key
	}
}

// Analyze returns an individual player's stats across all machines.
func Analyze(ctx context.Context, s Store, name string, opts ...Option) (*Result, error) {
	var o Options
	for _, opt := range opts {
		opt(&o)
	}

	leagueP50, err := s.GetLeagueP50(ctx)
	if err != nil {
		return nil, fmt.Errorf("load league averages: %w", err)
	}

	names, err := s.GetMachineNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("load machine names: %w", err)
	}

	if o.venue != "" {
		return playerAtVenue(ctx, s, name, o.venue, leagueP50, names)
	}

	stats, err := s.GetSinglePlayerMachineStats(ctx, name, "")
	if err != nil {
		return nil, fmt.Errorf("load player stats: %w", err)
	}

	var team *Team
	if pt, err := s.GetPlayerTeam(ctx, name); err == nil {
		team = &Team{Key: pt.TeamKey, Name: pt.TeamName}
	}

	return &Result{
		Name:        name,
		Team:        team,
		GlobalStats: enrichStats(stats, leagueP50, names),
		Analysis:    analyze(stats, leagueP50, names),
	}, nil
}

func playerAtVenue(ctx context.Context, s Store, name, venue string, leagueP50 map[string]float64, machineNames map[string]string) (*Result, error) {
	venueStats, err := s.GetSinglePlayerMachineStats(ctx, name, venue)
	if err != nil {
		return nil, fmt.Errorf("load player stats at venue: %w", err)
	}

	venueMachines, err := s.GetVenueMachines(ctx, venue)
	if err != nil {
		return nil, fmt.Errorf("load venue machines: %w", err)
	}

	globalStats, err := s.GetSinglePlayerMachineStats(ctx, name, "")
	if err != nil {
		return nil, fmt.Errorf("load player global stats: %w", err)
	}

	// Filter venue stats to machines at the venue.
	filtered := make([]db.PlayerMachineStats, 0, len(venueStats))
	for _, s := range venueStats {
		if venueMachines[s.MachineKey] {
			filtered = append(filtered, s)
		}
	}

	venueDataSet := make(map[string]bool, len(filtered))
	for _, s := range filtered {
		venueDataSet[s.MachineKey] = true
	}

	// Filter global stats to machines at the venue, flagging missing venue data.
	globalFiltered := make([]db.PlayerMachineStats, 0, len(globalStats))
	for _, s := range globalStats {
		if venueMachines[s.MachineKey] {
			globalFiltered = append(globalFiltered, s)
		}
	}

	global := make([]MachineStats, len(globalFiltered))
	for i, gs := range globalFiltered {
		global[i] = enrichStat(gs, leagueP50, machineNames)
		global[i].NoVenueData = !venueDataSet[gs.MachineKey]
	}

	var team *Team
	if pt, err := s.GetPlayerTeam(ctx, name); err == nil {
		team = &Team{Key: pt.TeamKey, Name: pt.TeamName}
	}

	return &Result{
		Name:        name,
		Venue:       venue,
		Team:        team,
		VenueStats:  enrichStats(filtered, leagueP50, machineNames),
		GlobalStats: global,
		Analysis:    analyze(globalStats, leagueP50, machineNames),
	}, nil
}

func enrichStats(stats []db.PlayerMachineStats, leagueP50 map[string]float64, names map[string]string) []MachineStats {
	result := make([]MachineStats, len(stats))
	for i, s := range stats {
		result[i] = enrichStat(s, leagueP50, names)
	}
	return result
}

func enrichStat(s db.PlayerMachineStats, leagueP50 map[string]float64, names map[string]string) MachineStats {
	return MachineStats{
		MachineKey:  s.MachineKey,
		MachineName: machineName(names, s.MachineKey),
		Games:       s.Games,
		P50Score:    s.P50Score,
		P90Score:    s.P90Score,
		LeagueP50:   leagueP50[s.MachineKey],
	}
}

func machineName(names map[string]string, key string) string {
	if n, ok := names[key]; ok {
		return n
	}
	return key
}

func analyze(stats []db.PlayerMachineStats, leagueP50 map[string]float64, names map[string]string) Analysis {
	sorted := make([]db.PlayerMachineStats, 0, len(stats))
	for _, s := range stats {
		if s.Games >= minGamesForAnalysis {
			sorted = append(sorted, s)
		}
	}

	slices.SortFunc(sorted, func(a, b db.PlayerMachineStats) int {
		aRel := relStr(a.P50Score, leagueP50[a.MachineKey])
		bRel := relStr(b.P50Score, leagueP50[b.MachineKey])
		return cmp.Compare(bRel, aRel)
	})

	var a Analysis
	for i := range min(3, len(sorted)) {
		a.Strongest = append(a.Strongest, machineName(names, sorted[i].MachineKey))
	}
	if len(sorted) > 3 {
		for i := len(sorted) - 1; i >= max(0, len(sorted)-3); i-- {
			a.Weakest = append(a.Weakest, machineName(names, sorted[i].MachineKey))
		}
	}
	return a
}

func relStr(p50, leagueP50 float64) float64 {
	if leagueP50 == 0 {
		return 0
	}
	return (p50 - leagueP50) / leagueP50 * 100
}
