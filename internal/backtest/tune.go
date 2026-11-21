package backtest

import (
	"cmp"

	"github.com/negz/mnp/internal/points"
)

// LogLoss returns the mean log loss of the predictor's probability that each
// player beats each player on the other team.
func LogLoss(games []Game, predict Predictor) float64 {
	var t Tally
	for _, g := range games {
		picker, responder := scores(g.Picker, g.League, predict), scores(g.Responder, g.League, predict)
		for i, a := range g.Picker {
			for j, b := range g.Responder {
				t.AddDuel(points.Beats(picker[i], responder[j]), (1+float64(cmp.Compare(a.Score, b.Score)))/2)
			}
		}
	}
	return t.Metrics().LogLoss
}

// A param is a model parameter to tune, with a step to start searching with
// and the least it can be.
type param struct {
	value *float64
	step  float64
	least float64
}

// Tune returns the model that best predicts who wins each head-to-head in the
// games, by log loss. It starts from m and searches one parameter at a time,
// halving the step it searches with on each pass.
func Tune(games []Game, m points.Model) points.Model {
	const passes = 6

	params := []param{
		{&m.Spread, 0.1, 0.2},
		{&m.LeagueGames, 8, 0.5},
		{&m.MachineGames, 8, 0.5},
		{&m.Unplayed, 0.05, -1},
		{&m.Picked, 0.05, -1},
		{&m.Home, 0.05, -1},
	}
	for i := range m.IPROffset {
		params = append(params, param{&m.IPROffset[i], 0.1, -3})
	}

	best := LogLoss(games, m.Score)
	for range passes {
		for i, p := range params {
			for improved := true; improved; {
				improved = false
				for _, d := range []float64{p.step, -p.step} {
					was := *p.value
					*p.value += d
					if *p.value < p.least {
						*p.value = was
						continue
					}
					if ll := LogLoss(games, m.Score); ll < best-1e-7 {
						best, improved = ll, true
						break
					}
					*p.value = was
				}
			}
			params[i].step /= 2
		}
	}
	return m
}
