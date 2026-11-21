package pick

import (
	"context"
	"math"
	"math/rand/v2"
	"slices"
)

// A plan is a pick for a round: a machine for each game, and our players for
// each. Machines and players are indexes into the machines that can be picked
// and our players.
type plan struct {
	machines []int
	players  [][]int
	points   float64 // Points our players expect against the best response.
	fixed    int     // The first fixed games were pinned, and can't change.
}

func (p plan) clone() plan {
	c := plan{machines: slices.Clone(p.machines), players: make([][]int, len(p.players)), points: p.points, fixed: p.fixed}
	for g := range p.players {
		c.players[g] = slices.Clone(p.players[g])
	}
	return c
}

// search returns the pick that wins our players the most points against the
// other team's best response, keeping the pinned games. There are far too many
// picks to try them all, so it climbs from several random picks to the best
// pick nearby, and returns the best it finds. Each climb finds the best pick
// near where it started, so more starts find the best pick more often, but take
// longer. Players who must play are always picked, if there are few enough of
// them. It stops if the context is done.
func search(ctx context.Context, machines, players, games, slots int, must []bool, pinned plan, starts int, r responder) (plan, error) {
	if countTrue(must) > games*slots {
		must = make([]bool, players)
	}
	// The same starts every time, so the same inputs get the same pick.
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // Search, not security.
	best := plan{points: math.Inf(-1)}
	for range starts {
		if err := ctx.Err(); err != nil {
			return plan{}, err
		}
		p := climb(start(rng, machines, players, games, slots, must, pinned), machines, players, must, r)
		if p.points > best.points {
			best = p
		}
	}
	return best, nil
}

// start returns a random pick that begins with the pinned games, and includes
// everyone who must play.
func start(rng *rand.Rand, machines, players, games, slots int, must []bool, pinned plan) plan {
	p := pinned.clone()
	p.fixed = len(pinned.machines)
	picked := make(map[int]bool)
	for _, m := range p.machines {
		picked[m] = true
	}
	playing := make(map[int]bool)
	for _, ps := range p.players {
		for _, pl := range ps {
			playing[pl] = true
		}
	}

	var order []int
	for _, pl := range rng.Perm(players) {
		if !playing[pl] {
			order = append(order, pl)
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		switch {
		case must[a] && !must[b]:
			return -1
		case must[b] && !must[a]:
			return 1
		}
		return 0
	})
	for _, m := range rng.Perm(machines) {
		if len(p.machines) == games {
			break
		}
		if !picked[m] {
			p.machines = append(p.machines, m)
			p.players = append(p.players, slices.Clone(order[:slots]))
			order = order[slots:]
		}
	}
	return p
}

// climb repeatedly makes the change to a pick that most improves it, until no
// change does.
func climb(p plan, machines, players int, must []bool, r responder) plan {
	p.points, _ = r.respond(p.machines, p.players)
	for {
		best := p
		for _, change := range changes(p, machines, players, must) {
			c := p.clone()
			change(c)
			if c.points, _ = r.respond(c.machines, c.players); c.points > best.points+1e-9 {
				best = c
			}
		}
		if best.points <= p.points {
			return p
		}
		p = best
	}
}

// changes returns the changes that could be made to the games in a pick that
// aren't pinned: swapping players
// between games, swapping machines between doubles games, and swapping in a
// machine, or a player who needn't play, that isn't picked.
func changes(p plan, machines, players int, must []bool) []func(plan) {
	var out []func(plan)
	for g1 := p.fixed; g1 < len(p.players); g1++ {
		for g2 := g1 + 1; g2 < len(p.players); g2++ {
			for s1 := range p.players[g1] {
				for s2 := range p.players[g2] {
					out = append(out, func(c plan) { c.players[g1][s1], c.players[g2][s2] = c.players[g2][s2], c.players[g1][s1] })
				}
			}
			// In singles swapping machines is the same as swapping
			// players, but in doubles it moves a whole pair.
			if len(p.players[g1]) > 1 {
				out = append(out, func(c plan) { c.machines[g1], c.machines[g2] = c.machines[g2], c.machines[g1] })
			}
		}
	}
	return append(out, substitutions(p, machines, players, must)...)
}

// substitutions returns the changes that swap a machine, or a player who
// needn't play, for one that isn't picked.
func substitutions(p plan, machines, players int, must []bool) []func(plan) {
	picked := make(map[int]bool)
	for _, m := range p.machines {
		picked[m] = true
	}
	playing := make(map[int]bool)
	for _, ps := range p.players {
		for _, pl := range ps {
			playing[pl] = true
		}
	}

	var out []func(plan)
	for g := p.fixed; g < len(p.players); g++ {
		for s, pl := range p.players[g] {
			for u := range players {
				if !must[pl] && !playing[u] {
					out = append(out, func(c plan) { c.players[g][s] = u })
				}
			}
		}
		for m := range machines {
			if !picked[m] {
				out = append(out, func(c plan) { c.machines[g] = m })
			}
		}
	}
	return out
}
