// Package live reads the state of matches in progress.
package live

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State is where a match is in its flow. A round moves from picking, to
// responding once the picking team has chosen its machines and players, to
// playing once the responding team has chosen its players.
type State string

// Match states.
const (
	StateScheduled  State = "scheduled"
	StatePregame    State = "pregame"
	StateLineups    State = "lineups"
	StatePicking    State = "picking"
	StateResponding State = "responding"
	StatePlaying    State = "playing"
	StateReviewing  State = "reviewing"
	StateComplete   State = "complete"
	StateTieBreaker State = "tie-breaker"
)

// A Match is a match as mondaynightpinball.com serves it.
type Match struct {
	Key    string  `json:"key"`
	State  State   `json:"state"`
	Round  int     `json:"round"`
	Home   Team    `json:"home"`
	Away   Team    `json:"away"`
	Rounds []Round `json:"rounds"`
}

// A Team is one side of a match.
type Team struct {
	Key    string   `json:"key"`
	Name   string   `json:"name"`
	Lineup []Player `json:"lineup"`
}

// A Player is in a team's lineup for a match.
type Player struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Sub       bool   `json:"sub"`
	IPR       int    `json:"IPR"`        //nolint:tagliatelle // Matches the upstream JSON.
	NumPlayed int    `json:"num_played"` //nolint:tagliatelle // Matches the upstream JSON.
}

// A Round is one of a match's four rounds.
type Round struct {
	N     int    `json:"n"`
	Games []Game `json:"games"`
}

// A Game is one game in a round. Players are referenced by key. The team that
// picked the machine plays as players 1 and 3.
type Game struct {
	N       int    `json:"n"`
	Machine string `json:"machine"`
	Player1 string `json:"player_1"` //nolint:tagliatelle // Matches the upstream JSON.
	Player2 string `json:"player_2"` //nolint:tagliatelle // Matches the upstream JSON.
	Player3 string `json:"player_3"` //nolint:tagliatelle // Matches the upstream JSON.
	Player4 string `json:"player_4"` //nolint:tagliatelle // Matches the upstream JSON.
	Done    bool   `json:"done"`
}

// Picker returns the team that picks the machines for the given round. The away
// team picks rounds 1 and 3, and the home team picks rounds 2 and 4.
func (m *Match) Picker(round int) *Team {
	if round%2 == 1 {
		return &m.Away
	}
	return &m.Home
}

// Responder returns the team that responds to the other team's picks in the
// given round.
func (m *Match) Responder(round int) *Team {
	if round%2 == 1 {
		return &m.Home
	}
	return &m.Away
}

// CurrentRound returns the round in progress, or nil if there isn't one.
func (m *Match) CurrentRound() *Round {
	for i := range m.Rounds {
		if m.Rounds[i].N == m.Round {
			return &m.Rounds[i]
		}
	}
	return nil
}

// Player returns the player in either team's lineup with the given key.
func (m *Match) Player(key string) (Player, bool) {
	for _, t := range []Team{m.Home, m.Away} {
		for _, p := range t.Lineup {
			if p.Key == key {
				return p, true
			}
		}
	}
	return Player{}, false
}

// A Source gets matches by key.
type Source interface {
	Match(ctx context.Context, key string) (*Match, error)
}

// A DirSource reads matches from JSON files named for their keys. It's for
// trying out the live view without a match in progress.
type DirSource struct {
	Dir string
}

// Match reads the match with the given key.
func (s DirSource) Match(_ context.Context, key string) (*Match, error) {
	f, err := os.Open(filepath.Join(s.Dir, key+".json")) //nolint:gosec // Development-only directory.
	if err != nil {
		return nil, fmt.Errorf("open match %s: %w", key, err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.

	m := &Match{}
	if err := json.NewDecoder(f).Decode(m); err != nil {
		return nil, fmt.Errorf("decode match %s: %w", key, err)
	}
	return m, nil
}
