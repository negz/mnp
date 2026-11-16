// Package matchup provides head-to-head team comparison at a venue.
package matchup

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/output"
)

// Store is the set of queries needed for matchup comparison.
type Store interface {
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

// MachineMatchup is a head-to-head comparison for a single machine.
type MachineMatchup struct {
	MachineKey      string
	MachineName     string
	Team1Likely     float64 // Average P50 of team 1's top two likely players.
	Team1Players    []LikelyPlayer
	Team1Picks      int // Games team 1's current players picked this machine for, ever.
	Team1Confidence Confidence
	Team2Likely     float64 // Average P50 of team 2's top two likely players.
	Team2Players    []LikelyPlayer
	Team2Picks      int // Games team 2's current players picked this machine for, ever.
	Team2Confidence Confidence
	Favorite        string  // The team whose likely players typically score more.
	Verdict         Verdict // How the machine is likely to go for the favorite.
	Edge            float64 // % by which one team's likely score exceeds the other's. Positive favors team 1.
}

// Confidence indicates how much data backs a team's likely score.
type Confidence int

// Confidence levels based on a team's top likely players' average game counts.
const (
	ConfidenceLow    Confidence = iota // Fewer than 3 games.
	ConfidenceMedium                   // 3-9 games.
	ConfidenceHigh                     // 10+ games.
)

// Verdict is how a machine is likely to go for the favorite.
type Verdict int

// Verdicts, from least to most certain.
const (
	VerdictUnknown Verdict = iota // Either team has low confidence.
	VerdictEven                   // Neither team's likely score is 1.2 times the other's.
	VerdictWin                    // The favorite's likely score is at least 1.2 times the other's.
	VerdictDouble                 // The favorite's likely score is at least double, enough to win a singles game 3-0.
)

// Result is the output of a Matchup query.
type Result struct {
	Venue    string
	Team1    string
	Team2    string
	Machines []MachineMatchup // Sorted for team 1 to pick from.
}

// Option configures a Matchup query.
type Option func(*Options)

// Options holds optional parameters for a Matchup query.
type Options struct{}

// Analyze compares two teams head-to-head at a venue.
func Analyze(ctx context.Context, s Store, venue, team1, team2 string, _ ...Option) (*Result, error) {
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

	picks1, err := s.GetRosterPicks(ctx, team1)
	if err != nil {
		return nil, fmt.Errorf("load picks for %s: %w", team1, err)
	}

	picks2, err := s.GetRosterPicks(ctx, team2)
	if err != nil {
		return nil, fmt.Errorf("load picks for %s: %w", team2, err)
	}

	byMachine1 := make(map[string]db.TeamMachineStats, len(stats1))
	for _, s := range stats1 {
		byMachine1[s.MachineKey] = s
	}
	byMachine2 := make(map[string]db.TeamMachineStats, len(stats2))
	for _, s := range stats2 {
		byMachine2[s.MachineKey] = s
	}

	machines := make([]MachineMatchup, 0, len(venueMachines))
	for key := range venueMachines {
		s1, ok1 := byMachine1[key]
		s2, ok2 := byMachine2[key]
		if !ok1 && !ok2 {
			continue
		}

		m := MachineMatchup{
			MachineKey:      key,
			MachineName:     output.MachineName(names, key),
			Team1Likely:     likelyScore(s1.LikelyPlayers),
			Team1Players:    likelyPlayers(s1.LikelyPlayers),
			Team1Picks:      picks1[key],
			Team1Confidence: confidence(s1.LikelyPlayers),
			Team2Likely:     likelyScore(s2.LikelyPlayers),
			Team2Players:    likelyPlayers(s2.LikelyPlayers),
			Team2Picks:      picks2[key],
			Team2Confidence: confidence(s2.LikelyPlayers),
		}
		m.Favorite, m.Verdict = call(team1, team2, m.Team1Likely, m.Team2Likely, m.Team1Confidence, m.Team2Confidence)
		m.Edge = edgePct(m.Team1Likely, m.Team2Likely)
		machines = append(machines, m)
	}

	sortForPicking(machines, team1)

	return &Result{
		Venue:    venue,
		Team1:    team1,
		Team2:    team2,
		Machines: machines,
	}, nil
}

// sortForPicking orders machines for team 1 to pick from: its likely wins, then
// machines it's favored on without enough data to call, then evens, then
// machines it's not favored on without enough data, then its likely losses.
// Machines in each group are ordered by edge.
func sortForPicking(machines []MachineMatchup, team1 string) {
	rank := func(m MachineMatchup) int {
		ours := m.Favorite == team1
		switch m.Verdict {
		case VerdictWin, VerdictDouble:
			if ours {
				return 0
			}
			return 4
		case VerdictEven:
			return 2
		case VerdictUnknown:
		}
		if ours {
			return 1
		}
		return 3
	}
	slices.SortFunc(machines, func(a, b MachineMatchup) int {
		return cmp.Or(
			cmp.Compare(rank(a), rank(b)),
			cmp.Compare(b.Edge, a.Edge),
			cmp.Compare(a.MachineName, b.MachineName),
		)
	})
}

// call returns the team whose likely players typically score more on a
// machine, and how the machine is likely to go for that team.
func call(team1, team2 string, l1, l2 float64, c1, c2 Confidence) (string, Verdict) {
	favorite, hi, lo := team1, l1, l2
	if l2 > l1 {
		favorite, hi, lo = team2, l2, l1
	}
	if c1 == ConfidenceLow || c2 == ConfidenceLow {
		return favorite, VerdictUnknown
	}
	switch {
	case hi >= 2*lo:
		return favorite, VerdictDouble
	case hi >= 1.2*lo:
		return favorite, VerdictWin
	default:
		return favorite, VerdictEven
	}
}

// edgePlayers is how many of a team's likely players decide its likely score
// and confidence on a machine.
const edgePlayers = 2

// likelyPlayers converts a team's likely players for a machine.
func likelyPlayers(players []db.LikelyPlayer) []LikelyPlayer {
	if len(players) == 0 {
		return nil
	}
	out := make([]LikelyPlayer, len(players))
	for i, p := range players {
		out[i] = LikelyPlayer{Name: p.Name, Games: p.Games, P50Score: p.P50Score, P90Score: p.P90Score, IPR: p.IPR}
	}
	return out
}

// likelyScore returns the average P50 of a team's top likely players.
func likelyScore(players []db.LikelyPlayer) float64 {
	players = players[:min(edgePlayers, len(players))]
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

// confidence returns how much data backs a team's likely score on a machine.
func confidence(players []db.LikelyPlayer) Confidence {
	players = players[:min(edgePlayers, len(players))]
	var avg float64
	if len(players) > 0 {
		var total int
		for _, p := range players {
			total += p.Games
		}
		avg = float64(total) / float64(len(players))
	}

	switch {
	case avg >= 10:
		return ConfidenceHigh
	case avg >= 3:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}
