package backtest

import (
	"math"
	"slices"

	"github.com/negz/mnp/internal/habit"
	"github.com/negz/mnp/internal/live"
	"github.com/negz/mnp/internal/mnp"
	"github.com/negz/mnp/internal/points"
)

// A Response is how a team responded to the other team's picks in a round,
// with what was known before the night it was played.
type Response struct {
	Season  int
	Doubles bool

	// Features is what was known about sending each of the responding
	// team's players to each game, indexed by player, then game. Expected is
	// the points the points model expected each player to win there,
	// counting only their own head-to-heads.
	Features [][]habit.Features
	Expected [][]float64

	Sent []int // The game each player was sent to.
}

// Responses replays every match in date order, and returns how each team
// responded in each round, as [Replay] does for games. Rounds with a game
// missing a player, or where a player scored 1 or less, are skipped.
func Responses(seasons []mnp.SeasonData, halfLife float64, m points.Model) []Response {
	var out []Response
	walk(seasons, halfLife, func(h *history, mt match) {
		byRound := make(map[int][]mnp.GameData)
		for _, g := range mt.Games {
			byRound[g.Round] = append(byRound[g.Round], g)
		}
		for round := range live.Rounds {
			games := byRound[round+1]
			if len(games) < 2 || slices.ContainsFunc(games, func(g mnp.GameData) bool { return !complete(g) }) {
				continue
			}
			out = append(out, h.Response(mt, games, m))
		}
	})
	return out
}

// Response returns how a team responded to a round's games.
func (h *history) Response(mt match, games []mnp.GameData, m points.Model) Response {
	r := Response{Season: mt.season, Doubles: games[0].IsDoubles}

	league := make([]float64, len(games))
	picker := make([][]points.Score, len(games))
	facing := make([]float64, len(games))
	var responders []mnp.ResultData
	for g, game := range games {
		league[g] = h.League(game.MachineKey, mt.season, mt.rostered)
		for _, res := range game.Results {
			if res.Position%2 == 0 {
				responders = append(responders, res)
				r.Sent = append(r.Sent, g)
				continue
			}
			p := h.Player(res, game.MachineKey, mt.season, mt.rostered).Known
			p.Picked, p.Home = true, res.IsHome
			picker[g] = append(picker[g], m.Score(p, league[g]))
			facing[g] += float64(res.IPR)
		}
		facing[g] /= float64(len(picker[g]))
	}

	for _, res := range responders {
		fs := make([]habit.Features, len(games))
		es := make([]float64, len(games))
		for g, game := range games {
			p := h.Player(res, game.MachineKey, mt.season, mt.rostered).Known
			p.Home = res.IsHome
			s := m.Score(p, league[g])
			f := habit.Features{
				IPR:        res.IPR,
				Facing:     facing[g],
				Games:      h.played[playedKey{player: res.PlayerName, machine: game.MachineKey}],
				VenueGames: h.playedVenue[playedKey{player: res.PlayerName, machine: game.MachineKey, venue: mt.Venue.Key}],
			}
			if p.Games > 0 && p.P50 > 0 && league[g] > 0 {
				f.Affinity = math.Log(p.P50/league[g]) - p.Relative
			}
			for _, t := range picker[g] {
				f.Win += points.Beats(s, t) / float64(len(picker[g]))
			}
			es[g] = f.Win * float64(len(picker[g]))
			if !r.Doubles {
				es[g] = points.Singles(s, picker[g][0])
			}
			fs[g] = f
		}
		r.Features = append(r.Features, fs)
		r.Expected = append(r.Expected, es)
	}
	return r
}

// features returns the features a habit model weighs, in the order of its
// fields.
func features(f habit.Features) []float64 {
	return []float64{float64(f.IPR) * f.Facing, math.Log1p(float64(f.Games)), math.Log1p(float64(f.VenueGames)), f.Affinity, f.Win}
}

// FitHabits returns the habit model that best explains how teams arranged the
// players they sent in each response. For every two players sent to different
// games, it asks how much likelier the model makes their arrangement than the
// one with them swapped, and maximises the likelihood of what teams chose
// (logistic regression, solved by Newton's method).
func FitHabits(rs []Response) habit.Model {
	// A singles round has 21 pairs of players, and a doubles round 24.
	swaps := make([][]float64, 0, 24*len(rs))
	for _, r := range rs {
		swaps = append(swaps, swapped(r)...)
	}

	const iterations = 25
	n := len(features(habit.Features{}))
	w := make([]float64, n)
	for range iterations {
		grad := make([]float64, n)
		hess := make([][]float64, n)
		for i := range hess {
			hess[i] = make([]float64, n)
		}
		for _, d := range swaps {
			var z float64
			for k := range d {
				z += w[k] * d[k]
			}
			p := 1 / (1 + math.Exp(-z))
			for a := range d {
				grad[a] += (1 - p) * d[a]
				for b := range d {
					hess[a][b] += p * (1 - p) * d[a] * d[b]
				}
			}
		}
		for k, s := range solve(hess, grad) {
			w[k] += s
		}
	}
	return habit.Model{IPRMatch: w[0], Games: w[1], VenueGames: w[2], Affinity: w[3], Win: w[4]}
}

// solve returns x such that a x = b, by Gaussian elimination.
func solve(a [][]float64, b []float64) []float64 {
	n := len(b)
	m := make([][]float64, n)
	for i := range m {
		m[i] = append(slices.Clone(a[i]), b[i])
	}
	for c := range n {
		p := c
		for r := c + 1; r < n; r++ {
			if math.Abs(m[r][c]) > math.Abs(m[p][c]) {
				p = r
			}
		}
		m[c], m[p] = m[p], m[c]
		for r := range n {
			if r == c {
				continue
			}
			f := m[r][c] / m[c][c]
			for k := c; k <= n; k++ {
				m[r][k] -= f * m[c][k]
			}
		}
	}
	x := make([]float64, n)
	for i := range n {
		x[i] = m[i][n] / m[i][i]
	}
	return x
}

// Placed returns the share of players sent to the game a placement predicts,
// given who was sent. A placement returns a utility for sending a player to a
// game, and predicts that each team arranged its players to get the most.
func Placed(rs []Response, utility func(r Response, player, game int) float64) float64 {
	var hit, n int
	for _, r := range rs {
		predicted := arrange(r, func(p, g int) float64 { return utility(r, p, g) })
		for p, g := range r.Sent {
			n++
			if predicted[p] == g {
				hit++
			}
		}
	}
	return float64(hit) / float64(n)
}

// arrange returns the game for each player that gets the most utility in total,
// sending as many players to each game as were sent. It works game by game,
// remembering the most utility from each set of players sent so far.
func arrange(r Response, utility func(player, game int) float64) []int {
	players, games := len(r.Sent), len(r.Features[0])
	slots := players / games
	size := 1 << players
	options, value := sendable(players, games, slots, utility)

	most := make([][]float64, games+1)
	chose := make([][]int, games+1)
	for g := range most {
		most[g] = make([]float64, size)
		chose[g] = make([]int, size)
		for mask := range most[g] {
			most[g][mask] = math.Inf(-1)
		}
	}
	most[0][0] = 0
	for g := range games {
		for mask, v := range most[g] {
			for i, sent := range options {
				if math.IsInf(v, -1) || sent&mask != 0 {
					continue
				}
				if total := v + value[g][i]; total > most[g+1][mask|sent] {
					most[g+1][mask|sent], chose[g+1][mask|sent] = total, sent
				}
			}
		}
	}

	out := make([]int, players)
	for g, mask := games, size-1; g > 0; g-- {
		for p := range players {
			if chose[g][mask]&(1<<p) != 0 {
				out[p] = g - 1
			}
		}
		mask &^= chose[g][mask]
	}
	return out
}

// swapped returns, for every two players sent to different games in a
// response, how much more of each feature their arrangement has than the one
// with them swapped.
func swapped(r Response) [][]float64 {
	var out [][]float64
	for i := range r.Sent {
		for j := i + 1; j < len(r.Sent); j++ {
			a, b := r.Sent[i], r.Sent[j]
			if a == b {
				continue
			}
			d := features(r.Features[i][a])
			jb, ib, ja := features(r.Features[j][b]), features(r.Features[i][b]), features(r.Features[j][a])
			for k := range d {
				d[k] += jb[k] - ib[k] - ja[k]
			}
			out = append(out, d)
		}
	}
	return out
}

// sendable returns who could be sent to a game, as a bit for each player, and
// the utility of sending them to each game.
func sendable(players, games, slots int, utility func(player, game int) float64) ([]int, [][]float64) {
	var options []int
	for sent := range 1 << players {
		n := 0
		for p := range players {
			n += sent >> p & 1
		}
		if n == slots {
			options = append(options, sent)
		}
	}
	value := make([][]float64, games)
	for g := range value {
		value[g] = make([]float64, len(options))
		for i, sent := range options {
			for p := range players {
				if sent&(1<<p) != 0 {
					value[g][i] += utility(p, g)
				}
			}
		}
	}
	return options, value
}
