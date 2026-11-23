package pick

import (
	"context"

	"github.com/negz/mnp/internal/habit"
	"github.com/negz/mnp/internal/points"
)

// missingCost is what the other team gives up, as they see it, by leaving a
// game short of a player. It's far more than any player's utility, so a short
// lineup sends everyone it can.
const missingCost = 100

// habitual returns how the other team prefers to respond if it responds as
// captains usually do, sending the players it most wants to send by the habit
// model. Our players are who their players would face.
func habitual(m habit.Model, ours, theirs scored) preference {
	return func(machine int, o, t []int) float64 {
		var facing float64
		for _, p := range o {
			facing += float64(ours.stats[p][machine].IPR)
		}
		facing /= float64(len(o))

		cost := missingCost * float64(len(o)-len(t))
		for _, p := range t {
			f := theirs.habits[p][machine]
			f.Facing = facing
			for _, q := range o {
				f.Win += points.Beats(theirs.scores[p][machine], ours.scores[q][machine]) / float64(len(o))
			}
			cost -= m.Utility(f)
		}
		return cost
	}
}

// hedgeMargin is the most points a pick may give up, if the other team responds
// as well as it can, to win more against how they likely will. Even captains
// who use mnp.tips tune its suggestions with their team, so the margin leaves
// room to pick for their habits.
const hedgeMargin = 0.3

// hedge returns the pick that wins our players the most points against the
// other team's likely response, among picks that give up at most hedgeMargin
// points against its best response, compared to the safest pick. Like search,
// it may not find the very best pick, but never returns one that does worse
// against the likely response than the safest pick it finds.
func hedge(ctx context.Context, machines, players, games, slots int, must []bool, pinned plan, starts int, likely, best responder) (plan, error) {
	safest, err := search(ctx, machines, players, games, slots, must, pinned, starts, best)
	if err != nil {
		return plan{}, err
	}
	h := hedged{likely: likely, best: best, floor: safest.points - hedgeMargin}
	p, err := search(ctx, machines, players, games, slots, must, pinned, starts, h)
	if err != nil {
		return plan{}, err
	}
	if v, _ := h.respond(safest.machines, safest.players); v >= p.points {
		p = safest
		p.points = v
	}
	return p, nil
}

// shortPenalty is far more than a round's points. A pick that falls short of
// the floor counts as worth less than nothing, so it's worse than any pick that
// doesn't, and worse still the further short it falls.
const shortPenalty = 1000

// hedged responds as the other team likely would, but counts a pick that does
// worse than a floor against the best response as worth less than nothing.
type hedged struct {
	likely, best responder
	floor        float64
}

func (h hedged) respond(machines []int, players [][]int) (float64, [][]int) {
	if b, _ := h.best.respond(machines, players); b < h.floor {
		return -shortPenalty - (h.floor - b), nil
	}
	return h.likely.respond(machines, players)
}

func (h hedged) points(machine int, ours, theirs []int) float64 {
	return h.likely.points(machine, ours, theirs)
}
