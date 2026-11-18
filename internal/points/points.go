// Package points estimates how players will score on a machine, and the points
// they can expect to win against each other.
//
// A player's score on a machine is modelled as log-normal. Pinball scores span
// orders of magnitude, and who beats whom depends on ratios of scores rather
// than differences, so log scores are the natural scale. The distribution has
// a heavier left tail than a normal one, from early drains, but predicted win
// probabilities are still calibrated: when the model gives a favourite 60%,
// 70%, 80% or 90%, they win about that often.
//
// The model's parameters were tuned with the backtest package, which replays
// the league's history and scores how well the model predicts who wins each
// head-to-head. Some things that look like improvements made predictions
// worse or no better, so the model deliberately leaves them out:
//
//   - A spread for each machine. Spread does vary a lot, from about 0.35 on
//     some machines to 1.8 on others, but so do the gaps between players, and
//     modelling it made predictions worse.
//   - Each player's own spread on a machine.
//   - The mean of a player's log scores instead of their median, which drains
//     drag down.
//   - Extra weight for scores at the venue being played. Players who have
//     played a machine there do score higher, but no more than players who
//     have played the machine anywhere, which Unplayed already covers.
//   - Shorter or longer half-lives for weighting recent scores, which made
//     no difference above two seasons.
package points

import (
	"cmp"
	"math"
)

// A Model estimates a player's score on a machine. Pinball scores vary a lot
// from game to game, so a score is modelled as log-normal.
//
// A player's typical score starts from the league's typical score on the
// machine, offset by their IPR. That moves toward how they typically score
// relative to the league across every machine as they play more league games,
// then toward their own typical score on the machine as they play it more.
type Model struct {
	// IPROffset is how a player's typical score compares with the league's,
	// in log terms, indexed by IPR. Unrated players have IPR 0.
	IPROffset [7]float64

	// Spread is the standard deviation of a player's log score.
	Spread float64

	// LeagueGames is how many league games of evidence a player's IPR is
	// worth, against how they score relative to the league.
	LeagueGames float64

	// MachineGames is how many games of evidence the estimate from IPR and
	// league games is worth, against the player's own scores on the machine.
	MachineGames float64

	// Unplayed is how much lower, in log terms, a player scores on a machine
	// they haven't played in the league before.
	Unplayed float64

	// Picked is how much higher, in log terms, players score on machines
	// their team picked.
	Picked float64

	// Home is how much higher, in log terms, players score at home.
	Home float64
}

// DefaultModel returns the model used to make suggestions. Its parameters were
// tuned by replaying seasons 16 to 22 with `mnp backtest --tune`, then rounded.
// Pinball is noisy enough that a player's own scores on a machine take about 40
// games to outweigh what their IPR and other league games say.
//
// NOTE(negz): The IPR offsets are fitted, not chosen, which is why they aren't
// evenly spaced. Unrated players (IPR 0) are mostly new subs, and play like IPR
// 1 players, as the league's rules assume.
func DefaultModel() Model {
	return Model{
		IPROffset:    [7]float64{-0.48, -0.43, -0.04, 0.16, 0.27, 0.57, 0.92},
		Spread:       0.9,
		LeagueGames:  20,
		MachineGames: 40,
		Unplayed:     0.12,
		Picked:       0.06,
		Home:         0.08,
	}
}

// A Player is what's known about a player in a game.
type Player struct {
	IPR    int
	Home   bool // Whether they're playing at their team's home venue.
	Picked bool // Whether their team picked the machine.

	// Games is how many games they've played on the machine, and P50 their
	// median score on it.
	Games int
	P50   float64

	// LeagueGames is how many games they've played on machines the league
	// has a typical score for. Relative is the median, across those games,
	// of the log ratio of their score to the league's typical score on the
	// machine they played.
	LeagueGames int
	Relative    float64
}

// A Score is a log-normal estimate of a player's score on a machine.
type Score struct {
	Mu    float64 // Mean of the log score.
	Sigma float64 // Standard deviation of the log score.
}

// Score returns a player's likely score on a machine, given the league's
// typical score on it. Without a typical score for the league it falls back to
// the player's own, and failing that guesses 1.
func (m Model) Score(p Player, league float64) Score {
	if p.P50 <= 0 {
		p.Games = 0
	}
	if league <= 0 {
		var mu float64
		if p.Games > 0 {
			mu = math.Log(p.P50)
		}
		return Score{Mu: mu, Sigma: m.Spread}
	}

	// IPR comes from the live match, so it isn't guaranteed to be in range.
	offset := m.IPROffset[min(max(p.IPR, 0), len(m.IPROffset)-1)]
	if p.LeagueGames > 0 {
		offset = blend(p.Relative, p.LeagueGames, offset, m.LeagueGames)
	}
	mu := math.Log(league) + offset
	if p.Games > 0 {
		mu = blend(math.Log(p.P50), p.Games, mu, m.MachineGames)
	} else {
		mu -= m.Unplayed
	}
	if p.Picked {
		mu += m.Picked
	}
	if p.Home {
		mu += m.Home
	}
	return Score{Mu: mu, Sigma: m.Spread}
}

// blend returns an estimate from n games, moved toward a prior worth k games.
func blend(estimate float64, n int, prior, k float64) float64 {
	return (float64(n)*estimate + k*prior) / (float64(n) + k)
}

// Beats returns the probability that a scores more than b.
func Beats(a, b Score) float64 {
	s := math.Hypot(a.Sigma, b.Sigma)
	if s == 0 {
		// Neither score varies, e.g. two missing players.
		return (1 + float64(cmp.Compare(a.Mu, b.Mu))) / 2
	}
	return phi((a.Mu - b.Mu) / s)
}

// Singles returns the points a player expects from a singles game. A player
// wins 3-0 by at least doubling the other player's score, or 2-1 by beating it.
// A loser who isn't doubled gets 1.
func Singles(ours, theirs Score) float64 {
	d := (ours.Mu - theirs.Mu) / math.Hypot(ours.Sigma, theirs.Sigma)
	t := math.Ln2 / math.Hypot(ours.Sigma, theirs.Sigma)
	doubled := 1 - phi(t-d)
	won := 1 - phi(-d)
	notDoubled := 1 - phi(-t-d)
	return 3*doubled + 2*(won-doubled) + (notDoubled - won)
}

// Doubles returns the points a pair expects from a doubles game. Each player
// gets a point for each player on the other team they beat, and the pair with
// the higher combined score gets one more.
func Doubles(ours, theirs []Score) float64 {
	ours, theirs = pair(ours), pair(theirs)
	var points float64
	for _, o := range ours {
		for _, t := range theirs {
			points += Beats(o, t)
		}
	}
	return points + Beats(Sum(ours), Sum(theirs))
}

// pair fills out a doubles team that's missing a player. The league scores a
// missing player 1 in every game, which is a log score of 0 that never varies.
func pair(scores []Score) []Score {
	if len(scores) >= 2 {
		return scores
	}
	out := make([]Score, 2)
	copy(out, scores)
	return out
}

// Sum approximates the sum of log-normal scores as another log-normal score,
// matching its mean and variance (the Fenton-Wilkinson approximation).
func Sum(scores []Score) Score {
	var mean, variance float64
	for _, s := range scores {
		m := math.Exp(s.Mu + s.Sigma*s.Sigma/2)
		mean += m
		variance += (math.Exp(s.Sigma*s.Sigma) - 1) * m * m
	}
	if mean == 0 {
		return Score{}
	}
	s2 := math.Log(1 + variance/(mean*mean))
	return Score{Mu: math.Log(mean) - s2/2, Sigma: math.Sqrt(s2)}
}

// phi is the standard normal cumulative distribution function.
func phi(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt2))
}
