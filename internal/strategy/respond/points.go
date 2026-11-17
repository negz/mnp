package respond

import "math"

// Pinball scores vary a lot from game to game, so a player's score on a machine
// is modelled as log-normal. These constants come from the league's history.
const (
	// scoreSpread is the median standard deviation of log scores, for players
	// with 10 or more games on a machine.
	scoreSpread = 0.85

	// priorGames is how many games of evidence the prior for a player's
	// typical score is worth. Their own typical score outweighs it after a
	// couple of games.
	priorGames = 2

	// priorSpreadGames is how many games of evidence the default spread is
	// worth. A player's own spread needs more games to pin down than their
	// typical score.
	priorSpreadGames = 5

	// z90 is the number of standard deviations from the median to the 90th
	// percentile of a normal distribution.
	z90 = 1.2816
)

// iprOffset is how a player's typical score on a machine compares with the
// league's, in log terms, by IPR. An IPR 6 player typically scores about 2.6
// times the league's typical score.
var iprOffset = map[int]float64{ //nolint:gochecknoglobals // Lookup table.
	1: -0.27,
	2: -0.02,
	3: 0.14,
	4: 0.28,
	5: 0.54,
	6: 0.96,
}

// A score is a log-normal estimate of a player's score on a machine.
type score struct {
	mu    float64 // Mean of the log score.
	sigma float64 // Standard deviation of the log score.
}

// estimate returns a player's likely score on a machine. It starts from the
// league's typical score adjusted for the player's IPR, and moves toward the
// player's own scores as they play the machine more.
func estimate(p Player, league float64) score {
	if p.P50Score <= 0 {
		p.Games = 0
	}
	n := float64(p.Games)

	var mu float64
	switch {
	case p.Games > 0 && league > 0:
		prior := math.Log(league) + iprOffset[p.IPR]
		mu = (n*math.Log(p.P50Score) + priorGames*prior) / (n + priorGames)
	case p.Games > 0:
		mu = math.Log(p.P50Score)
	case league > 0:
		mu = math.Log(league) + iprOffset[p.IPR]
	}

	sigma := scoreSpread
	if p.Games >= 2 && p.P90Score > p.P50Score {
		own := (math.Log(p.P90Score) - math.Log(p.P50Score)) / z90
		sigma = (n*own + priorSpreadGames*scoreSpread) / (n + priorSpreadGames)
	}
	return score{mu: mu, sigma: sigma}
}

// beats returns the probability that a scores more than b.
func beats(a, b score) float64 {
	return phi((a.mu - b.mu) / math.Hypot(a.sigma, b.sigma))
}

// singlesPoints returns the points a player expects from a singles game. A
// player wins 3-0 by at least doubling the other player's score, or 2-1 by
// beating it. A loser who isn't doubled gets 1.
func singlesPoints(ours, theirs score) float64 {
	d := (ours.mu - theirs.mu) / math.Hypot(ours.sigma, theirs.sigma)
	t := math.Ln2 / math.Hypot(ours.sigma, theirs.sigma)
	doubled := 1 - phi(t-d)
	won := 1 - phi(-d)
	notDoubled := 1 - phi(-t-d)
	return 3*doubled + 2*(won-doubled) + (notDoubled - won)
}

// doublesPoints returns the points a pair expects from a doubles game. Each
// player gets a point for each player on the other team they beat, and the pair
// with the higher combined score gets one more.
func doublesPoints(ours, theirs []score) float64 {
	var points float64
	for _, o := range ours {
		for _, t := range theirs {
			points += beats(o, t)
		}
	}
	return points + beats(sum(ours), sum(theirs))
}

// sum approximates the sum of log-normal scores as another log-normal score,
// matching its mean and variance (the Fenton-Wilkinson approximation).
func sum(scores []score) score {
	var mean, variance float64
	for _, s := range scores {
		m := math.Exp(s.mu + s.sigma*s.sigma/2)
		mean += m
		variance += (math.Exp(s.sigma*s.sigma) - 1) * m * m
	}
	if mean == 0 {
		return score{}
	}
	s2 := math.Log(1 + variance/(mean*mean))
	return score{mu: math.Log(mean) - s2/2, sigma: math.Sqrt(s2)}
}

// phi is the standard normal cumulative distribution function.
func phi(x float64) float64 {
	return 0.5 * (1 + math.Erf(x/math.Sqrt2))
}
