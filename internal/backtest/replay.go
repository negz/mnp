// Package backtest replays the league's history to measure how well the points
// model predicts games, and to tune it.
//
// Replay walks every match in the archive in date order. For each game it
// rebuilds what the live response model would have known before that night,
// using only earlier nights, so predictions are never informed by the games
// they predict.
//
// The headline measure is log loss on head-to-heads: for each game, the
// predicted probability that each player beats each player on the other team,
// scored against who did. A singles game is one head-to-head and a doubles
// game four. Lower is better, and predicting a coin flip every time scores
// ln 2, about 0.693. Evaluate also reports Brier score, bias, and the error in
// the picking team's expected points, overall and in slices. The favourite
// slices show calibration. Seen from the favourite's side, a bias near zero in
// each slice means the predicted probabilities can be taken at face value.
//
// Seasons 14 and 15 are only history, because too few players have earlier
// games for predictions there to mean much. [points.DefaultModel] was tuned on
// seasons 16 to 22 with Tune, which searches one parameter at a time to
// minimise log loss. Seasons 23 and 24 were held out from all of that, and
// scored once at the end:
//
//	Everyone even                   0.693
//	Model before tuning             0.659
//	IPR only, with tuned offsets    0.633
//	points.DefaultModel             0.618
//
// To check a change to the model, run `mnp backtest` for seasons 16 to 22 and
// compare log loss and calibration. Only use seasons 23 and 24, with
// `--from 23 --to 24`, for a final check, or they stop being held out.
package backtest

import (
	"cmp"
	"math"
	"slices"

	"github.com/negz/mnp/internal/mnp"
	"github.com/negz/mnp/internal/points"
	"github.com/negz/mnp/internal/recency"
)

// A Player is one player's part in a game, with what was known about them
// before the night it was played.
type Player struct {
	Name   string
	Known  points.Player
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
// known beforehand, as the live response model would have known it: each
// player's IPR from that night's lineup, whether they're home and whether
// their team picked the machine, their recency weighted P50 on the machine, and
// how they score relative to the league across machines. Everything comes from
// earlier nights. The league's P50 on a machine counts players on that
// season's rosters.
//
// Games missing a player, or where a player scored 1 or less (a forfeit or
// disqualification), are skipped, and their scores aren't used as evidence.
func Replay(seasons []mnp.SeasonData, halfLife float64) []Game {
	var games []Game
	walk(seasons, halfLife, func(h *history, m match) {
		for _, g := range m.Games {
			if complete(g) {
				games = append(games, h.Game(m.season, m.rostered, m.MatchData, g))
			}
		}
	})
	return games
}

// A match is a match from the archive, with its season and who was on that
// season's rosters.
type match struct {
	mnp.MatchData

	season   int
	rostered map[string]bool
}

// walk calls visit for every match in date order, with the history of every
// earlier night.
func walk(seasons []mnp.SeasonData, halfLife float64, visit func(h *history, m match)) {
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

	h := newHistory(halfLife)
	for len(matches) > 0 {
		n := 1
		for n < len(matches) && matches[n].Date == matches[0].Date {
			n++
		}
		night := matches[:n]
		matches = matches[n:]

		for _, m := range night {
			visit(h, m)
		}
		for _, m := range night {
			h.Add(m.season, m.MatchData)
		}
	}
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

// A playedKey is a player and a machine, and for games at one venue, the
// venue.
type playedKey struct {
	player, machine, venue string
}

// history is the scores from the nights replayed so far.
type history struct {
	halfLife  float64
	byPlayer  map[string]map[string][]recency.Game // By player, then machine.
	byMachine map[string][]scored

	// How many games each player has played on each machine, anywhere and
	// at each venue. Like the live stats, these count forfeits and
	// disqualifications.
	played      map[playedKey]int
	playedVenue map[playedKey]int

	// The league's typical score on each machine, cached until scores are
	// added. It depends on the season's rosters, so it's cached by season.
	league map[leagueKey]float64
}

// newHistory returns an empty history, weighting scores with the given
// half-life in seasons.
func newHistory(halfLife float64) *history {
	return &history{
		halfLife:    halfLife,
		byPlayer:    make(map[string]map[string][]recency.Game),
		byMachine:   make(map[string][]scored),
		played:      make(map[playedKey]int),
		playedVenue: make(map[playedKey]int),
		league:      make(map[leagueKey]float64),
	}
}

// Add adds a match's scores to the history.
func (h *history) Add(season int, m mnp.MatchData) {
	clear(h.league)
	for _, g := range m.Games {
		for _, r := range g.Results {
			h.played[playedKey{player: r.PlayerName, machine: g.MachineKey}]++
			h.playedVenue[playedKey{player: r.PlayerName, machine: g.MachineKey, venue: m.Venue.Key}]++
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
		picked := r.Position%2 == 1
		p := h.Player(r, g.MachineKey, season, rostered)
		p.Known.Picked = picked
		p.Known.Home = r.IsHome
		if picked {
			out.Picker = append(out.Picker, p)
		} else {
			out.Responder = append(out.Responder, p)
		}
	}
	return out
}

// Player returns a player's result, with what was known about them as of the
// given season.
func (h *history) Player(r mnp.ResultData, machine string, season int, rostered map[string]bool) Player {
	games := h.byPlayer[r.PlayerName][machine]

	// How the player scores relative to the league's typical score now,
	// on every machine they've played that the league has a typical score
	// for.
	var relative []recency.Game
	for m, gs := range h.byPlayer[r.PlayerName] {
		league := h.League(m, season, rostered)
		if league <= 0 {
			continue
		}
		for _, g := range gs {
			relative = append(relative, recency.Game{Season: g.Season, Score: math.Log(g.Score / league)})
		}
	}

	return Player{
		Name: r.PlayerName,
		Known: points.Player{
			IPR:         r.IPR,
			Games:       len(games),
			P50:         recency.Percentile(games, season, h.halfLife, 0.5),
			LeagueGames: len(relative),
			Relative:    recency.Percentile(relative, season, h.halfLife, 0.5),
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
