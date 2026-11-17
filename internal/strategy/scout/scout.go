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
	GetRosterPicks(ctx context.Context, teamKey string) (map[string]int, error)
	GetVenueMachines(ctx context.Context, venueKey string) (map[string]bool, error)
}

// LikelyPlayer is a player likely to play a machine.
type LikelyPlayer struct {
	Name     string
	Games    int
	P50Score float64
	P90Score float64
	IPR      int
}

// MachineStats is a team's performance on a single machine.
type MachineStats struct {
	MachineKey    string
	MachineName   string
	Games         int
	Picks         int // Games the team's current players picked this machine for, ever.
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

	var venueMachines map[string]bool
	if o.venue != "" {
		venueMachines, err = s.GetVenueMachines(ctx, o.venue)
		if err != nil {
			return nil, fmt.Errorf("load venue machines: %w", err)
		}
	}

	stats, err := s.GetTeamMachineStats(ctx, team, "")
	if err != nil {
		return nil, fmt.Errorf("load team stats: %w", err)
	}

	picks, err := s.GetRosterPicks(ctx, team)
	if err != nil {
		return nil, fmt.Errorf("load roster picks: %w", err)
	}

	if o.venue != "" {
		filtered := make([]db.TeamMachineStats, 0, len(stats))
		for _, ts := range stats {
			if venueMachines[ts.MachineKey] {
				filtered = append(filtered, ts)
			}
		}
		stats = filtered
	}

	return &Result{
		Team:        team,
		Venue:       o.venue,
		GlobalStats: enrichStats(stats, leagueP50, names, picks),
		Analysis:    analyze(stats, leagueP50, names),
	}, nil
}

func enrichStats(stats []db.TeamMachineStats, leagueP50 map[string]float64, names map[string]string, picks map[string]int) []MachineStats {
	result := make([]MachineStats, len(stats))
	for i, s := range stats {
		result[i] = enrichStat(s, leagueP50, names, picks)
	}
	return result
}

func enrichStat(s db.TeamMachineStats, leagueP50 map[string]float64, names map[string]string, picks map[string]int) MachineStats {
	ms := MachineStats{
		MachineKey:  s.MachineKey,
		MachineName: output.MachineName(names, s.MachineKey),
		Games:       s.Games,
		Picks:       picks[s.MachineKey],
		P50Score:    s.P50Score,
		P90Score:    s.P90Score,
		LeagueP50:   leagueP50[s.MachineKey],
	}
	for _, lp := range s.LikelyPlayers {
		ms.LikelyPlayers = append(ms.LikelyPlayers, LikelyPlayer{
			Name:     lp.Name,
			Games:    lp.Games,
			P50Score: lp.P50Score,
			P90Score: lp.P90Score,
			IPR:      lp.IPR,
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
