// Package recency weights games by how long ago they were played, so that
// recent games count for more than older ones.
package recency

import (
	"cmp"
	"math"
	"slices"
)

// Each half-life is how many seasons it takes for a game to count half as much.
const (
	// PlayHalfLife is for who's likely to play a machine, which should mostly
	// reflect who plays it now.
	PlayHalfLife = 0.5

	// ScoreHalfLife is for how well someone plays a machine, which changes
	// slowly, so older scores still count for a lot.
	ScoreHalfLife = 4.0
)

// A Game is a score, and the season it was scored in.
type Game struct {
	Season int
	Score  float64
}

// Weight returns how much a game played in the given season counts, relative to
// a game played in the latest season.
func Weight(season, latest int, halfLife float64) float64 {
	return math.Exp2(-float64(latest-season) / halfLife)
}

// Plays returns how many games were played, with older games counting for
// less.
func Plays(games []Game, latest int, halfLife float64) float64 {
	var total float64
	for _, g := range games {
		total += Weight(g.Season, latest, halfLife)
	}
	return total
}

// Percentile returns the lowest score that at least fraction p of games score
// at or below, with older games counting for less. With games from a single
// season it's the plain percentile: the median of three games is the second
// lowest score.
func Percentile(games []Game, latest int, halfLife, p float64) float64 {
	if len(games) == 0 {
		return 0
	}
	sorted := slices.SortedFunc(slices.Values(games), func(a, b Game) int {
		return cmp.Compare(a.Score, b.Score)
	})
	var total float64
	for _, g := range sorted {
		total += Weight(g.Season, latest, halfLife)
	}
	// Weights for older seasons aren't exact in floating point, so their sum
	// can fall just short of the target. Allow for that, so that e.g. the
	// median of six games from last season is still the third lowest score.
	target := p * total * (1 - 1e-9)
	var sum float64
	for _, g := range sorted {
		sum += Weight(g.Season, latest, halfLife)
		if sum >= target {
			return g.Score
		}
	}
	return sorted[len(sorted)-1].Score
}
