// Package scout provides team scouting analysis.
package scout

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/output"
)

const minGamesForAnalysis = 3

// Store is the set of queries needed for scouting.
type Store interface {
	GetLeagueP50(ctx context.Context) (map[string]float64, error)
	GetMachineNames(ctx context.Context) (map[string]string, error)
	GetTeamMachineStats(ctx context.Context, teamKey, venueKey string) ([]db.TeamMachineStats, error)
	GetVenueMachines(ctx context.Context, venueKey string) (map[string]bool, error)
}

// LikelyPlayer is a player likely to play a machine.
type LikelyPlayer struct {
	Name     string
	Games    int
	P50Score float64
}

// MachineStats is a team's performance on a single machine.
type MachineStats struct {
	MachineKey    string
	MachineName   string
	Games         int
	P50Score      float64
	P90Score      float64
	LeagueP50     float64
	LikelyPlayers []LikelyPlayer
}

// Analysis summarizes a team's strongest and weakest machines.
type Analysis struct {
	Strongest []string // Machine names, up to 3.
	Weakest   []string // Machine names, up to 3.
}

// Result is the output of a Scout query.
type Result struct {
	Team        string
	Venue       string         // Empty for global-only queries.
	GlobalStats []MachineStats // All machines, or filtered to venue machines when a venue is set.
	Analysis    Analysis
}

// Option configures a Scout query.
type Option func(*Options)

// Options holds optional parameters for a Scout query.
type Options struct {
	venue string
}

// AtVenue filters scouting to a specific venue.
func AtVenue(key string) Option {
	return func(o *Options) {
		o.venue = key
	}
}

// Analyze returns a team's strengths and weaknesses across machines.
func Analyze(ctx context.Context, s Store, team string, opts ...Option) (*Result, error) {
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
		return scoutVenue(ctx, s, team, o.venue, leagueP50, names)
	}

	stats, err := s.GetTeamMachineStats(ctx, team, "")
	if err != nil {
		return nil, fmt.Errorf("load team stats: %w", err)
	}

	return &Result{
		Team:        team,
		GlobalStats: enrichStats(stats, leagueP50, names),
		Analysis:    analyze(stats, leagueP50, names),
	}, nil
}

func scoutVenue(ctx context.Context, s Store, team, venue string, leagueP50 map[string]float64, names map[string]string) (*Result, error) {
	venueMachines, err := s.GetVenueMachines(ctx, venue)
	if err != nil {
		return nil, fmt.Errorf("load venue machines: %w", err)
	}

	globalStats, err := s.GetTeamMachineStats(ctx, team, "")
	if err != nil {
		return nil, fmt.Errorf("load team stats: %w", err)
	}

	// Filter global stats to machines at the venue.
	filtered := make([]db.TeamMachineStats, 0, len(globalStats))
	for _, gs := range globalStats {
		if venueMachines[gs.MachineKey] {
			filtered = append(filtered, gs)
		}
	}

	return &Result{
		Team:        team,
		Venue:       venue,
		GlobalStats: enrichStats(filtered, leagueP50, names),
		Analysis:    analyze(filtered, leagueP50, names),
	}, nil
}

func enrichStats(stats []db.TeamMachineStats, leagueP50 map[string]float64, names map[string]string) []MachineStats {
	result := make([]MachineStats, len(stats))
	for i, s := range stats {
		result[i] = enrichStat(s, leagueP50, names)
	}
	return result
}

func enrichStat(s db.TeamMachineStats, leagueP50 map[string]float64, names map[string]string) MachineStats {
	ms := MachineStats{
		MachineKey:  s.MachineKey,
		MachineName: output.MachineName(names, s.MachineKey),
		Games:       s.Games,
		P50Score:    s.P50Score,
		P90Score:    s.P90Score,
		LeagueP50:   leagueP50[s.MachineKey],
	}
	for _, lp := range s.LikelyPlayers {
		ms.LikelyPlayers = append(ms.LikelyPlayers, LikelyPlayer{
			Name:     lp.Name,
			Games:    lp.Games,
			P50Score: lp.P50Score,
		})
	}
	return ms
}

// analyze computes strongest/weakest machines by relative strength.
func analyze(stats []db.TeamMachineStats, leagueP50 map[string]float64, names map[string]string) Analysis {
	sorted := make([]db.TeamMachineStats, 0, len(stats))
	for _, s := range stats {
		if s.Games >= minGamesForAnalysis {
			sorted = append(sorted, s)
		}
	}

	slices.SortFunc(sorted, func(a, b db.TeamMachineStats) int {
		aRel := output.RelStr(a.P50Score, leagueP50[a.MachineKey])
		bRel := output.RelStr(b.P50Score, leagueP50[b.MachineKey])
		return cmp.Compare(bRel, aRel)
	})

	var a Analysis
	for i := range min(3, len(sorted)) {
		a.Strongest = append(a.Strongest, output.MachineName(names, sorted[i].MachineKey))
	}
	if len(sorted) > 3 {
		for i := len(sorted) - 1; i >= max(0, len(sorted)-3); i-- {
			a.Weakest = append(a.Weakest, output.MachineName(names, sorted[i].MachineKey))
		}
	}
	return a
}
