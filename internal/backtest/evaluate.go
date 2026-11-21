package backtest

import (
	"cmp"
	"fmt"
	"math"
	"slices"

	"github.com/negz/mnp/internal/points"
)

// A Predictor estimates a player's score on a machine, given what's known
// about them and the league's typical score on it.
type Predictor func(p points.Player, league float64) points.Score

// Even predicts every player will score the same.
func Even(_ points.Player, _ float64) points.Score {
	return points.Score{Sigma: 1}
}

// IPROnly predicts players will score like others with their IPR, ignoring
// their own games on the machine.
func IPROnly(m points.Model) Predictor {
	return func(p points.Player, league float64) points.Score {
		return m.Score(points.Player{IPR: p.IPR}, league)
	}
}

// Metrics measure how well a predictor did.
type Metrics struct {
	// Head-to-head outcomes: whether each player beat each player on the
	// other team. A tie counts as half a win.
	Duels   int
	LogLoss float64
	Brier   float64
	Bias    float64 // How much more often the picking team, or the favourite, won than predicted.

	// Points the picking team won in each game.
	Games      int
	PointsMAE  float64
	PointsBias float64 // How many more points the picking team won than predicted.
}

// A Tally accumulates metrics.
type Tally struct {
	duels, games                int
	logLoss, brier, bias        float64
	pointsAbsErr, pointsBiasSum float64
}

// AddDuel adds the outcome of one player playing another, given the predicted
// probability p that the first player won. won is 1 if they did, 0.5 for a
// tie, and 0 otherwise.
func (t *Tally) AddDuel(p, won float64) {
	const eps = 1e-9
	q := min(max(p, eps), 1-eps)
	t.duels++
	t.logLoss -= won*math.Log(q) + (1-won)*math.Log(1-q)
	t.brier += (p - won) * (p - won)
	t.bias += won - p
}

// AddGame adds the points a team was predicted to win in a game, and the points
// it won.
func (t *Tally) AddGame(predicted, actual float64) {
	t.games++
	t.pointsAbsErr += math.Abs(actual - predicted)
	t.pointsBiasSum += actual - predicted
}

// Metrics returns the metrics accumulated so far.
func (t *Tally) Metrics() Metrics {
	m := Metrics{Duels: t.duels, Games: t.games}
	if t.duels > 0 {
		n := float64(t.duels)
		m.LogLoss, m.Brier, m.Bias = t.logLoss/n, t.brier/n, t.bias/n
	}
	if t.games > 0 {
		n := float64(t.games)
		m.PointsMAE, m.PointsBias = t.pointsAbsErr/n, t.pointsBiasSum/n
	}
	return m
}

// A Slice is a subset of games, and how well a predictor did on them.
type Slice struct {
	Metrics

	Dimension string
	Value     string
}

// Evaluate returns how well the predictor did on the games, overall and sliced
// by format, by season, by how many games the least experienced player had
// played on the machine, and by the IPR gap between players.
func Evaluate(games []Game, predict Predictor) []Slice {
	type key struct{ dim, value string }
	var order []key
	tallies := make(map[key]*Tally)
	tally := func(dim, value string) *Tally {
		k := key{dim, value}
		if tallies[k] == nil {
			tallies[k] = &Tally{}
			order = append(order, k)
		}
		return tallies[k]
	}

	// Create slices in display order.
	tally("all", "")
	tally("format", "singles")
	tally("format", "doubles")
	for _, b := range experience {
		tally("games", b.label)
	}
	for gap := range 7 {
		tally("ipr gap", fmt.Sprint(gap))
	}
	for tenths := 5; tenths < 10; tenths++ {
		tally("favourite", confidence(float64(tenths)/10))
	}
	for _, g := range games {
		tally("season", fmt.Sprint(g.Season))
	}

	for _, g := range games {
		picker, responder := scores(g.Picker, g.League, predict), scores(g.Responder, g.League, predict)

		format := "singles"
		predicted := points.Singles(picker[0], responder[0])
		if g.Doubles {
			format = "doubles"
			predicted = points.Doubles(picker, responder)
		}
		var actual float64
		for _, p := range g.Picker {
			actual += p.Points
		}

		least := slices.MinFunc(slices.Concat(g.Picker, g.Responder), func(a, b Player) int {
			return cmp.Compare(a.Known.Games, b.Known.Games)
		})
		gameSlices := []*Tally{
			tally("all", ""),
			tally("format", format),
			tally("season", fmt.Sprint(g.Season)),
			tally("games", bucket(least.Known.Games)),
		}
		for _, t := range gameSlices {
			t.AddGame(predicted, actual)
		}

		for i, a := range g.Picker {
			for j, b := range g.Responder {
				p := points.Beats(picker[i], responder[j])
				won := (1 + float64(cmp.Compare(a.Score, b.Score))) / 2
				duelSlices := []*Tally{
					tally("all", ""),
					tally("format", format),
					tally("season", fmt.Sprint(g.Season)),
					tally("games", bucket(min(a.Known.Games, b.Known.Games))),
					tally("ipr gap", fmt.Sprint(abs(a.Known.IPR-b.Known.IPR))),
				}
				for _, t := range duelSlices {
					t.AddDuel(p, won)
				}

				// Seen from the favourite's side, the bias by how
				// confident the prediction was shows whether it's
				// over or under confident.
				if p < 0.5 {
					p, won = 1-p, 1-won
				}
				tally("favourite", confidence(p)).AddDuel(p, won)
			}
		}
	}

	out := make([]Slice, 0, len(order))
	for _, k := range order {
		out = append(out, Slice{Metrics: tallies[k].Metrics(), Dimension: k.dim, Value: k.value})
	}
	return out
}

// experience buckets how many games a player has played on a machine.
var experience = []struct { //nolint:gochecknoglobals // Lookup table.
	label string
	min   int
}{
	{"0", 0},
	{"1-2", 1},
	{"3-9", 3},
	{"10+", 10},
}

// scores returns the predicted score of each player.
func scores(players []Player, league float64, predict Predictor) []points.Score {
	out := make([]points.Score, len(players))
	for i, p := range players {
		out[i] = predict(p.Known, league)
	}
	return out
}

// confidence buckets the probability that the favourite wins.
func confidence(p float64) string {
	lo := min(math.Floor(p*10+1e-9), 9) * 10
	return fmt.Sprintf("%.0f-%.0f%%", lo, lo+10)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func bucket(games int) string {
	label := experience[0].label
	for _, b := range experience {
		if games >= b.min {
			label = b.label
		}
	}
	return label
}
