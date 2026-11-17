// Package points estimates how players will score on a machine, and the points
// they can expect to win against each other.
package points

import "math"

// z90 is the number of standard deviations from the median to the 90th
// percentile of a normal distribution.
const z90 = 1.2816

// A Model estimates a player's score on a machine. Pinball scores vary a lot
// from game to game, so a score is modelled as log-normal.
type Model struct {
	// IPROffset is how a player's typical score on a machine compares with
	// the league's, in log terms, indexed by IPR.
	IPROffset [7]float64

	// Spread is the standard deviation of a player's log scores, before
	// their own scores say otherwise.
	Spread float64

	// PriorGames is how many games of evidence the prior for a player's
	// typical score is worth.
	PriorGames float64

	// PriorSpreadGames is how many games of evidence Spread is worth.
	PriorSpreadGames float64
}

// DefaultModel returns the model used to make suggestions. Its constants come
// from the league's history.
func DefaultModel() Model {
	return Model{
		// An IPR 6 player typically scores about 2.6 times the league's
		// typical score.
		IPROffset: [7]float64{0, -0.27, -0.02, 0.14, 0.28, 0.54, 0.96},

		// The median standard deviation of log scores, for players with 10
		// or more games on a machine.
		Spread: 0.85,

		// A player's own typical score outweighs the prior after a couple
		// of games.
		PriorGames: 2,

		// A player's own spread needs more games to pin down than their
		// typical score.
		PriorSpreadGames: 5,
	}
}

// Stats are what's known about a player on a machine.
type Stats struct {
	IPR   int
	Games int // Games played on the machine.
	P50   float64
	P90   float64
}

// A Score is a log-normal estimate of a player's score on a machine.
type Score struct {
	Mu    float64 // Mean of the log score.
	Sigma float64 // Standard deviation of the log score.
}

// Score returns a player's likely score on a machine, given the league's
// typical score on it. It starts from the league's typical score adjusted for
// the player's IPR, and moves toward the player's own scores as they play the
// machine more.
func (m Model) Score(s Stats, league float64) Score {
	if s.P50 <= 0 {
		s.Games = 0
	}
	n := float64(s.Games)

	var mu float64
	switch {
	case s.Games > 0 && league > 0:
		prior := math.Log(league) + m.IPROffset[s.IPR]
		mu = (n*math.Log(s.P50) + m.PriorGames*prior) / (n + m.PriorGames)
	case s.Games > 0:
		mu = math.Log(s.P50)
	case league > 0:
		mu = math.Log(league) + m.IPROffset[s.IPR]
	}

	sigma := m.Spread
	if s.Games >= 2 && s.P90 > s.P50 {
		own := (math.Log(s.P90) - math.Log(s.P50)) / z90
		sigma = (n*own + m.PriorSpreadGames*m.Spread) / (n + m.PriorSpreadGames)
	}
	return Score{Mu: mu, Sigma: sigma}
}

// Beats returns the probability that a scores more than b.
func Beats(a, b Score) float64 {
	return phi((a.Mu - b.Mu) / math.Hypot(a.Sigma, b.Sigma))
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
	var points float64
	for _, o := range ours {
		for _, t := range theirs {
			points += Beats(o, t)
		}
	}
	return points + Beats(Sum(ours), Sum(theirs))
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
