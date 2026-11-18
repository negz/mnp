// Package backtest replays the league's history to measure how well the points
// model predicts games.
package backtest

import (
	"cmp"
	"slices"

	"github.com/negz/mnp/internal/mnp"
	"github.com/negz/mnp/internal/points"
	"github.com/negz/mnp/internal/recency"
)

// A Player is one player's part in a game, with what was known about them
// before the night it was played.
type Player struct {
	Name   string
	Stats  points.Stats
	Score  float64 // What they scored.
	Points float64 // Points they won, including any share of a team bonus.
}

// A Game is a game as it was played, with what was known before the night it
// was played.
type Game struct {
	Season    int
	Date      string
	Match     string
	Round     int
	Machine   string
	Doubles   bool
	League    float64  // The league's typical score on the machine.
	Picker    []Player // The team that picked the machine, as players 1 and 3.
	Responder []Player // The other team, as players 2 and 4.
}

// Replay replays every match in date order. For each game it returns what was
// known beforehand: each player's IPR from that night's lineup, their recency
// weighted P50 and P90 on the machine from earlier nights, and the league's
// P50 on the machine from earlier nights by players on that season's rosters.
// That's what the live response model would have known.
//
// Games missing a player, or where a player scored 1 or less (a forfeit or
// disqualification), are skipped, and their scores aren't used as evidence.
func Replay(seasons []mnp.SeasonData, halfLife float64) []Game {
	type match struct {
		mnp.MatchData

		season   int
		rostered map[string]bool
	}
	var matches []match
	for _, s := range seasons {
		rostered := make(map[string]bool)
		for _, t := range s.Teams {
			for _, name := range t.Roster {
				rostered[name] = true
			}
		}
		for _, m := range s.Matches {
			matches = append(matches, match{MatchData: m, season: s.Number, rostered: rostered})
		}
	}
	slices.SortStableFunc(matches, func(a, b match) int {
		return cmp.Or(cmp.Compare(a.Date, b.Date), cmp.Compare(a.Key, b.Key))
	})

	h := &history{
		halfLife:  halfLife,
		byPlayer:  make(map[string]map[string][]recency.Game),
		byMachine: make(map[string][]scored),
		league:    make(map[leagueKey]float64),
	}
	var games []Game
	for len(matches) > 0 {
		n := 1
		for n < len(matches) && matches[n].Date == matches[0].Date {
			n++
		}
		night := matches[:n]
		matches = matches[n:]

		for _, m := range night {
			for _, g := range m.Games {
				if complete(g) {
					games = append(games, h.Game(m.season, m.rostered, m.MatchData, g))
				}
			}
		}
		for _, m := range night {
			h.Add(m.season, m.MatchData)
		}
	}
	return games
}

// complete reports whether every player in a game played it.
func complete(g mnp.GameData) bool {
	want := 2
	if g.IsDoubles {
		want = 4
	}
	if len(g.Results) != want {
		return false
	}
	for _, r := range g.Results {
		if r.Score <= 1 {
			return false
		}
	}
	return true
}

// A scored game is a player's score on a machine.
type scored struct {
	recency.Game

	player string
}

type leagueKey struct {
	season  int
	machine string
}

// history is the scores from the nights replayed so far.
type history struct {
	halfLife  float64
	byPlayer  map[string]map[string][]recency.Game // By player, then machine.
	byMachine map[string][]scored

	// The league's typical score on each machine, cached until scores are
	// added. It depends on the season's rosters, so it's cached by season.
	league map[leagueKey]float64
}

// Add adds a match's scores to the history.
func (h *history) Add(season int, m mnp.MatchData) {
	clear(h.league)
	for _, g := range m.Games {
		for _, r := range g.Results {
			if r.Score <= 1 {
				continue
			}
			sg := recency.Game{Season: season, Score: float64(r.Score)}
			if h.byPlayer[r.PlayerName] == nil {
				h.byPlayer[r.PlayerName] = make(map[string][]recency.Game)
			}
			h.byPlayer[r.PlayerName][g.MachineKey] = append(h.byPlayer[r.PlayerName][g.MachineKey], sg)
			h.byMachine[g.MachineKey] = append(h.byMachine[g.MachineKey], scored{Game: sg, player: r.PlayerName})
		}
	}
}

// Game returns a game from a match in the given season, with what the history
// knows about its players and machine.
func (h *history) Game(season int, rostered map[string]bool, m mnp.MatchData, g mnp.GameData) Game {
	out := Game{
		Season:  season,
		Date:    m.Date,
		Match:   m.Key,
		Round:   g.Round,
		Machine: g.MachineKey,
		Doubles: g.IsDoubles,
		League:  h.League(g.MachineKey, season, rostered),
	}
	for _, r := range g.Results {
		p := h.Player(r, g.MachineKey, season)
		if r.Position%2 == 1 {
			out.Picker = append(out.Picker, p)
		} else {
			out.Responder = append(out.Responder, p)
		}
	}
	return out
}

// Player returns a player's result, with their stats on the machine as of the
// given season.
func (h *history) Player(r mnp.ResultData, machine string, season int) Player {
	games := h.byPlayer[r.PlayerName][machine]
	return Player{
		Name: r.PlayerName,
		Stats: points.Stats{
			IPR:   r.IPR,
			Games: len(games),
			P50:   recency.Percentile(games, season, h.halfLife, 0.5),
			P90:   recency.Percentile(games, season, h.halfLife, 0.9),
		},
		Score:  float64(r.Score),
		Points: r.Points,
	}
}

// League returns the league's typical score on a machine as of the given
// season, counting only rostered players.
func (h *history) League(machine string, season int, rostered map[string]bool) float64 {
	k := leagueKey{season: season, machine: machine}
	if v, ok := h.league[k]; ok {
		return v
	}
	var games []recency.Game
	for _, s := range h.byMachine[machine] {
		if rostered[s.player] {
			games = append(games, s.Game)
		}
	}
	h.league[k] = recency.Percentile(games, season, h.halfLife, 0.5)
	return h.league[k]
}
