package db

import (
	"cmp"
	"math"
	"slices"
)

// Recent games count for more than older ones. Each half-life is how many
// seasons it takes for a game to count half as much.
const (
	// Who's likely to play a machine should mostly reflect who plays it now.
	playHalfLife = 0.5

	// How well someone plays a machine changes slowly, so older scores still
	// count for a lot.
	scoreHalfLife = 4.0
)

// A scoredGame is a score, and the season it was scored in.
type scoredGame struct {
	season int
	score  float64
}

// recency returns how much a game played in the given season counts, relative
// to a game played in the latest season.
func recency(season, latest int, halfLife float64) float64 {
	return math.Exp2(-float64(latest-season) / halfLife)
}

// plays returns how many games were played, with older games counting for
// less.
func plays(games []scoredGame, latest int) float64 {
	var total float64
	for _, g := range games {
		total += recency(g.season, latest, playHalfLife)
	}
	return total
}

// percentile returns the lowest score that at least fraction p of games score
// at or below, with older games counting for less. With games from a single
// season it's the plain percentile: the median of three games is the second
// lowest score.
func percentile(games []scoredGame, latest int, p float64) float64 {
	if len(games) == 0 {
		return 0
	}
	sorted := slices.SortedFunc(slices.Values(games), func(a, b scoredGame) int {
		return cmp.Compare(a.score, b.score)
	})
	var total float64
	for _, g := range sorted {
		total += recency(g.season, latest, scoreHalfLife)
	}
	// Weights for older seasons aren't exact in floating point, so their sum
	// can fall just short of the target. Allow for that, so that e.g. the
	// median of six games from last season is still the third lowest score.
	target := p * total * (1 - 1e-9)
	var sum float64
	for _, g := range sorted {
		sum += recency(g.season, latest, scoreHalfLife)
		if sum >= target {
			return g.score
		}
	}
	return sorted[len(sorted)-1].score
}
