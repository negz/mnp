// Package matchup provides head-to-head team comparison at a venue.
package matchup

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/negz/mnp/internal/db"
)

// Store is the set of queries needed for matchup comparison.
type Store interface {
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

// MachineMatchup is a head-to-head comparison for a single machine.
type MachineMatchup struct {
	MachineName string
	Team1P50    float64
	Team1Likely float64 // Average P50 of team 1's likely players.
	Team2P50    float64
	Team2Likely float64 // Average P50 of team 2's likely players.
	Edge        float64 // Positive favors team 1.
	Confidence  Confidence
}

// Confidence indicates how much data backs a matchup edge.
type Confidence int

// Confidence levels based on likely players' average game counts.
const (
	ConfidenceLow    Confidence = iota // Either team's likely players average < 3 games.
	ConfidenceMedium                   // Both teams' likely players average 3-9 games.
	ConfidenceHigh                     // Both teams' likely players average 10+ games.
)

// Analysis summarizes which team has the advantage on which machines.
type Analysis struct {
	Team1Advantages []string // Machine names where team 1 has the edge.
	Team2Advantages []string // Machine names where team 2 has the edge.
	Contested       []string // Machine names where teams are even.
}

// Result is the output of a Matchup query.
type Result struct {
	Venue    string
	Team1    string
	Team2    string
	Machines []MachineMatchup // Sorted by edge descending (team 1's best first).
	Analysis Analysis
}

// Option configures a Matchup query.
type Option func(*Options)

// Options holds optional parameters for a Matchup query.
type Options struct{}

// Matchup compares two teams head-to-head at a venue.
func Matchup(ctx context.Context, s Store, venue, team1, team2 string, _ ...Option) (*Result, error) {
	venueMachines, err := s.GetVenueMachines(ctx, venue)
	if err != nil {
		return nil, fmt.Errorf("load venue machines: %w", err)
	}

	names, err := s.GetMachineNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("load machine names: %w", err)
	}

	stats1, err := s.GetTeamMachineStats(ctx, team1, "")
	if err != nil {
		return nil, fmt.Errorf("load stats for %s: %w", team1, err)
	}

	stats2, err := s.GetTeamMachineStats(ctx, team2, "")
	if err != nil {
		return nil, fmt.Errorf("load stats for %s: %w", team2, err)
	}

	stats2ByMachine := make(map[string]db.TeamMachineStats, len(stats2))
	for _, s := range stats2 {
		stats2ByMachine[s.MachineKey] = s
	}

	machines := make([]MachineMatchup, 0, len(stats1))
	for _, s1 := range stats1 {
		if !venueMachines[s1.MachineKey] {
			continue
		}

		s2 := stats2ByMachine[s1.MachineKey]
		l1 := likelyScore(s1.LikelyPlayers)
		l2 := likelyScore(s2.LikelyPlayers)

		machines = append(machines, MachineMatchup{
			MachineName: machineName(names, s1.MachineKey),
			Team1P50:    s1.P50Score,
			Team1Likely: l1,
			Team2P50:    s2.P50Score,
			Team2Likely: l2,
			Edge:        edgePct(l1, l2),
			Confidence:  confidence(s1.LikelyPlayers, s2.LikelyPlayers),
		})
		delete(stats2ByMachine, s1.MachineKey)
	}

	for key, s2 := range stats2ByMachine {
		if !venueMachines[key] {
			continue
		}

		l2 := likelyScore(s2.LikelyPlayers)
		machines = append(machines, MachineMatchup{
			MachineName: machineName(names, key),
			Team2P50:    s2.P50Score,
			Team2Likely: l2,
			Edge:        edgePct(0, l2),
			Confidence:  ConfidenceLow,
		})
	}

	slices.SortFunc(machines, func(a, b MachineMatchup) int {
		return cmp.Compare(b.Edge, a.Edge)
	})

	return &Result{
		Venue:    venue,
		Team1:    team1,
		Team2:    team2,
		Machines: machines,
		Analysis: analyze(machines),
	}, nil
}

func analyze(machines []MachineMatchup) Analysis {
	var a Analysis
	for _, m := range machines {
		switch {
		case m.Edge > 0:
			a.Team1Advantages = append(a.Team1Advantages, m.MachineName)
		case m.Edge < 0:
			a.Team2Advantages = append(a.Team2Advantages, m.MachineName)
		default:
			a.Contested = append(a.Contested, m.MachineName)
		}
	}
	return a
}

// likelyScore returns the average P50 of a team's likely players.
func likelyScore(players []db.LikelyPlayer) float64 {
	if len(players) == 0 {
		return 0
	}
	var sum float64
	for _, p := range players {
		sum += p.P50Score
	}
	return sum / float64(len(players))
}

// edgePct returns the % by which team 1's likely score exceeds team 2's.
// Positive means team 1 is stronger.
func edgePct(l1, l2 float64) float64 {
	lo := min(l1, l2)
	if lo == 0 {
		if l1 == l2 {
			return 0
		}
		return math.Copysign(math.MaxFloat64, l1-l2)
	}
	return (l1 - l2) / lo * 100
}

func confidence(players1, players2 []db.LikelyPlayer) Confidence {
	avg := func(players []db.LikelyPlayer) float64 {
		if len(players) == 0 {
			return 0
		}
		var total int
		for _, p := range players {
			total += p.Games
		}
		return float64(total) / float64(len(players))
	}

	minAvg := min(avg(players1), avg(players2))
	switch {
	case minAvg >= 10:
		return ConfidenceHigh
	case minAvg >= 3:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

func machineName(names map[string]string, key string) string {
	if n, ok := names[key]; ok {
		return n
	}
	return key
}
