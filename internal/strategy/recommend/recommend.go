// Package recommend provides player recommendations for a machine.
package recommend

import (
	"context"

	"github.com/negz/mnp/internal/db"
)

// Store is the set of queries needed for player recommendations.
type Store interface {
	GetLeagueP50(ctx context.Context) (map[string]float64, error)
	GetPlayerMachineStats(ctx context.Context, teamKey, machineKey, venueKey string) ([]db.PlayerStats, error)
}

// PlayerStats is a player's performance on the target machine.
type PlayerStats struct {
	Name        string
	Games       int
	P50Score    float64
	P90Score    float64
	LeagueP50   float64
	NoVenueData bool // True in global stats when this player has no venue-specific data.
}

// Assessment summarizes how the team's best compares to the opponent's best.
type Assessment struct {
	OurBest   string
	TheirBest string
	Diff      float64 // Positive means our best outscores theirs.
	Verdict   Verdict
}

// Verdict classifies the matchup on a specific machine.
type Verdict int

// Verdict values based on P50 difference between best players.
const (
	VerdictStrong    Verdict = iota // Our best outscores theirs by > 1M.
	VerdictWeak                     // Their best outscores ours by > 1M.
	VerdictContested                // Best players are roughly even.
)

// Result is the output of a Recommend query.
type Result struct {
	Team          string
	Machine       string
	Venue         string        // Empty if no venue filter.
	Opponent      string        // Empty if no opponent comparison.
	VenueStats    []PlayerStats // Nil if no venue filter.
	GlobalStats   []PlayerStats // Always populated (basic or global fallback).
	OpponentStats []PlayerStats // Nil if no opponent.
	Assessment    *Assessment   // Nil if no opponent or insufficient data.
}

// Option configures a Recommend query.
type Option func(*Options)

// Options holds optional parameters for a Recommend query.
type Options struct {
	venue    string
	opponent string
}

// AtVenue filters recommendations to a specific venue.
func AtVenue(key string) Option {
	return func(o *Options) {
		o.venue = key
	}
}

// VsOpponent adds opponent comparison.
func VsOpponent(key string) Option {
	return func(o *Options) {
		o.opponent = key
	}
}

// Recommend returns player recommendations for a team on a machine.
func Recommend(ctx context.Context, s Store, team, machine string, opts ...Option) (*Result, error) {
	var o Options
	for _, opt := range opts {
		opt(&o)
	}

	leagueP50, err := s.GetLeagueP50(ctx)
	if err != nil {
		return nil, err
	}
	lp50 := leagueP50[machine]

	if o.opponent != "" {
		return recommendVsOpponent(ctx, s, team, machine, o.venue, o.opponent, lp50)
	}

	if o.venue != "" {
		return recommendAtVenue(ctx, s, team, machine, o.venue, lp50)
	}

	stats, err := s.GetPlayerMachineStats(ctx, team, machine, "")
	if err != nil {
		return nil, err
	}

	return &Result{
		Team:        team,
		Machine:     machine,
		GlobalStats: enrichStats(stats, lp50),
	}, nil
}

func recommendAtVenue(ctx context.Context, s Store, team, machine, venue string, lp50 float64) (*Result, error) {
	venueStats, err := s.GetPlayerMachineStats(ctx, team, machine, venue)
	if err != nil {
		return nil, err
	}

	globalStats, err := s.GetPlayerMachineStats(ctx, team, machine, "")
	if err != nil {
		return nil, err
	}

	venuePlayerSet := make(map[string]bool, len(venueStats))
	for _, s := range venueStats {
		venuePlayerSet[s.Name] = true
	}

	global := enrichStats(globalStats, lp50)
	for i := range global {
		global[i].NoVenueData = !venuePlayerSet[global[i].Name]
	}

	return &Result{
		Team:        team,
		Machine:     machine,
		Venue:       venue,
		VenueStats:  enrichStats(venueStats, lp50),
		GlobalStats: global,
	}, nil
}

func recommendVsOpponent(ctx context.Context, s Store, team, machine, venue, opponent string, lp50 float64) (*Result, error) {
	ourStats, err := s.GetPlayerMachineStats(ctx, team, machine, venue)
	if err != nil {
		return nil, err
	}

	theirStats, err := s.GetPlayerMachineStats(ctx, opponent, machine, venue)
	if err != nil {
		return nil, err
	}

	r := &Result{
		Team:          team,
		Machine:       machine,
		Venue:         venue,
		Opponent:      opponent,
		GlobalStats:   enrichStats(ourStats, lp50),
		OpponentStats: enrichStats(theirStats, lp50),
	}

	if len(ourStats) > 0 && len(theirStats) > 0 {
		diff := ourStats[0].P50Score - theirStats[0].P50Score
		var v Verdict
		switch {
		case diff > 1_000_000:
			v = VerdictStrong
		case diff < -1_000_000:
			v = VerdictWeak
		default:
			v = VerdictContested
		}
		r.Assessment = &Assessment{
			OurBest:   ourStats[0].Name,
			TheirBest: theirStats[0].Name,
			Diff:      diff,
			Verdict:   v,
		}
	}

	return r, nil
}

func enrichStats(stats []db.PlayerStats, lp50 float64) []PlayerStats {
	result := make([]PlayerStats, len(stats))
	for i, s := range stats {
		result[i] = PlayerStats{
			Name:      s.Name,
			Games:     s.Games,
			P50Score:  s.P50Score,
			P90Score:  s.P90Score,
			LeagueP50: lp50,
		}
	}
	return result
}
