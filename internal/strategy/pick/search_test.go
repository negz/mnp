package pick

import (
	"context"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/negz/mnp/internal/points"
)

// randomScores returns how players might score on each machine, indexed by
// player then machine. Typical scores vary by a few times between players. The
// weak players typically score far less, so a pick or response that didn't
// have to include them wouldn't.
func randomScores(rng *rand.Rand, players, machines int, weak ...int) [][]points.Score {
	out := make([][]points.Score, players)
	for p := range out {
		out[p] = make([]points.Score, machines)
		for m := range out[p] {
			out[p][m] = points.Score{Mu: 18 + rng.NormFloat64(), Sigma: 0.9}
			if slices.Contains(weak, p) {
				out[p][m].Mu -= 3
			}
		}
	}
	return out
}

// permutations calls fn with every ordered choice of k items from n.
func permutations(n, k int, fn func([]int)) {
	chosen := make([]int, 0, k)
	used := make([]bool, n)
	var rec func()
	rec = func() {
		if len(chosen) == k {
			fn(chosen)
			return
		}
		for i := range n {
			if used[i] {
				continue
			}
			used[i] = true
			chosen = append(chosen, i)
			rec()
			chosen = chosen[:len(chosen)-1]
			used[i] = false
		}
	}
	rec()
}

func TestHungarian(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for range 50 {
		rows, cols := 1+rng.IntN(5), 5+rng.IntN(3)
		cost := make([][]float64, rows)
		for r := range cost {
			cost[r] = make([]float64, cols)
			for c := range cost[r] {
				cost[r][c] = rng.Float64()
			}
		}

		want := math.Inf(1)
		permutations(cols, rows, func(cs []int) {
			var total float64
			for r, c := range cs {
				total += cost[r][c]
			}
			want = min(want, total)
		})

		got := hungarian(cost)
		var total float64
		for r, c := range got {
			total += cost[r][c]
		}
		slices.Sort(got)
		if len(slices.Compact(got)) != rows {
			t.Fatalf("hungarian(%v): used a column twice: %v", cost, got)
		}
		if diff := cmp.Diff(want, total, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
			t.Errorf("hungarian(%v): -want cheapest total, +got:\n%s", cost, diff)
		}
	}
}

// bestSinglesResponse is the fewest points our players can be held to in a
// singles round, found by trying every response.
func bestSinglesResponse(s *singles, machines []int, players [][]int, must []bool) float64 {
	least := math.Inf(1)
	permutations(len(s.must), len(machines), func(ts []int) {
		sent := make(map[int]bool)
		var total float64
		for g, t := range ts {
			sent[t] = true
			total += s.expected[machines[g]][players[g][0]][t]
		}
		for t, m := range must {
			if m && !sent[t] {
				return
			}
		}
		least = min(least, total)
	})
	return least
}

func TestSinglesRespond(t *testing.T) {
	rng := rand.New(rand.NewPCG(5, 6))
	cases := map[string]struct {
		reason string
		theirs int
		must   []int
	}{
		"FullLineup": {
			reason: "The best response should hold our players to the fewest points.",
			theirs: 10,
		},
		"MustPlay": {
			reason: "The best response should send everyone who must play.",
			theirs: 10,
			must:   []int{8, 9},
		},
		"ShortLineup": {
			reason: "A lineup too short to fill every game should leave missing players in the rest.",
			theirs: 5,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ours, theirs := randomScores(rng, 10, 9), randomScores(rng, tc.theirs, 9, tc.must...)
			must := make([]bool, tc.theirs)
			for _, m := range tc.must {
				must[m] = true
			}
			s := newSingles(ours, theirs, must, singlesGames)
			machines := []int{0, 1, 2, 3, 4, 5, 6}
			players := [][]int{{0}, {1}, {2}, {3}, {4}, {5}, {6}}

			got, sent := s.respond(machines, players)
			want := bestSinglesResponse(s, machines, players, must)
			if diff := cmp.Diff(want, got, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nrespond(...): -want points, +got:\n%s", tc.reason, diff)
			}

			var total float64
			seen := make(map[int]bool)
			missing := 0
			for g, ts := range sent {
				total += s.points(machines[g], players[g], ts)
				if len(ts) == 0 {
					missing++
				}
				for _, p := range ts {
					seen[p] = true
				}
			}
			if diff := cmp.Diff(got, total, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nrespond(...): points against who's sent don't add up: -want, +got:\n%s", tc.reason, diff)
			}
			if want := max(0, singlesGames-tc.theirs); missing != want {
				t.Errorf("\n%s\nrespond(...): want %d games with nobody sent, got %d", tc.reason, want, missing)
			}
			for _, m := range tc.must {
				if !seen[m] {
					t.Errorf("\n%s\nrespond(...): player %d must play, but wasn't sent", tc.reason, m)
				}
			}
		})
	}
}

// bestDoublesResponse is the fewest points our players can be held to in a
// doubles round, found by trying every response.
func bestDoublesResponse(d *doubles, machines []int, players [][]int, must []bool) float64 {
	mask := 0
	for p, m := range must {
		if m {
			mask |= 1 << p
		}
	}
	least := math.Inf(1)
	var rec func(g, used int, total float64)
	rec = func(g, used int, total float64) {
		if g == len(machines) {
			if used&mask == mask {
				least = min(least, total)
			}
			return
		}
		for _, opt := range d.options {
			if opt.mask&used == 0 {
				rec(g+1, used|opt.mask, total+d.points(machines[g], players[g], opt.players))
			}
		}
	}
	rec(0, 0, 0)
	return least
}

func TestDoublesRespond(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	cases := map[string]struct {
		reason string
		theirs int
		must   []int
	}{
		"FullLineup": {
			reason: "The best response should hold our players to the fewest points.",
			theirs: 10,
		},
		"MustPlay": {
			reason: "The best response should send everyone who must play.",
			theirs: 10,
			must:   []int{7, 8, 9},
		},
		"ShortLineup": {
			reason: "A lineup too short to fill every game should still send everyone it has.",
			theirs: 7,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ours, theirs := randomScores(rng, 10, 6), randomScores(rng, tc.theirs, 6, tc.must...)
			must := make([]bool, tc.theirs)
			for _, m := range tc.must {
				must[m] = true
			}
			d := newDoubles(ours, theirs, must, doublesGames)
			machines := []int{0, 2, 3, 5}
			players := [][]int{{0, 1}, {2, 3}, {4, 5}, {6, 7}}

			got, sent := d.respond(machines, players)
			want := bestDoublesResponse(d, machines, players, must)
			if diff := cmp.Diff(want, got, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nrespond(...): -want points, +got:\n%s", tc.reason, diff)
			}

			var total float64
			seen := 0
			for g, ps := range sent {
				total += d.points(machines[g], players[g], ps)
				seen += len(ps)
			}
			if diff := cmp.Diff(got, total, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nrespond(...): points against who's sent don't add up: -want, +got:\n%s", tc.reason, diff)
			}
			if want := min(tc.theirs, 2*doublesGames); seen != want {
				t.Errorf("\n%s\nrespond(...): want %d players sent, got %d", tc.reason, want, seen)
			}
		})
	}
}

// bestPick is the most points our players can expect against the best
// response, found by trying every pick of machines and players.
func bestPick(r responder, machines, players, games, slots int, must []bool, pinned plan) float64 {
	best := math.Inf(-1)
	permutations(machines, games, func(ms []int) {
		// Machines are tried in every order, so players need only be
		// tried in one order per game.
		permutations(players, games*slots, func(ps []int) {
			for g := range games {
				if !slices.IsSorted(ps[g*slots : (g+1)*slots]) {
					return
				}
			}
			for p, m := range must {
				if m && !slices.Contains(ps, p) {
					return
				}
			}
			pl := make([][]int, games)
			for g := range games {
				pl[g] = ps[g*slots : (g+1)*slots]
			}
			for k, m := range pinned.machines {
				g := slices.Index(ms, m)
				if g < 0 || !slices.Equal(pl[g], pinned.players[k]) {
					return
				}
			}
			v, _ := r.respond(ms, pl)
			best = max(best, v)
		})
	})
	return best
}

func TestSearch(t *testing.T) {
	rng := rand.New(rand.NewPCG(9, 10))
	cases := map[string]struct {
		reason   string
		doubles  bool
		machines int
		ours     int
		theirs   int
		must     []int
		weak     []int
		pinned   plan
	}{
		"Singles": {
			reason:   "The search should find the best singles pick.",
			machines: 4,
			ours:     4,
			theirs:   4,
		},
		"SinglesMustPlay": {
			reason:   "The search should find the best singles pick that includes everyone who must play.",
			machines: 4,
			ours:     4,
			theirs:   4,
			must:     []int{3},
		},
		"SinglesPinned": {
			reason:   "The search should find the best singles pick around a pinned game, though it's a weak player on a machine they'd rather not play.",
			machines: 4,
			ours:     4,
			theirs:   4,
			weak:     []int{0},
			pinned:   plan{machines: []int{2}, players: [][]int{{0}}},
		},
		"DoublesPinned": {
			reason:   "The search should find the best doubles pick around a pinned game.",
			doubles:  true,
			machines: 3,
			ours:     5,
			theirs:   5,
			weak:     []int{4},
			pinned:   plan{machines: []int{1}, players: [][]int{{0, 4}}},
		},
		"Doubles": {
			reason:   "The search should find the best doubles pick.",
			doubles:  true,
			machines: 3,
			ours:     5,
			theirs:   5,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			ours, theirs := randomScores(rng, tc.ours, tc.machines, append(tc.must, tc.weak...)...), randomScores(rng, tc.theirs, tc.machines)
			must := make([]bool, tc.ours)
			for _, m := range tc.must {
				must[m] = true
			}
			// Small rounds, so every pick can be tried.
			games, slots := 3, 1
			var r responder = newSingles(ours, theirs, nil, games)
			if tc.doubles {
				games, slots = 2, 2
				r = newDoubles(ours, theirs, nil, games)
			}

			got, err := search(context.Background(), tc.machines, tc.ours, games, slots, must, tc.pinned, 12, r)
			if err != nil {
				t.Fatalf("search(...): %v", err)
			}
			want := bestPick(r, tc.machines, tc.ours, games, slots, must, tc.pinned)
			if diff := cmp.Diff(want, got.points, cmpopts.EquateApprox(0, 1e-9)); diff != "" {
				t.Errorf("\n%s\nsearch(...): -want points, +got:\n%s", tc.reason, diff)
			}
			if n := len(tc.pinned.machines); got.fixed != n || !slices.Equal(got.machines[:n], tc.pinned.machines) {
				t.Errorf("\n%s\nsearch(...): want the first %d games pinned to machines %v, got %d pinned to %v", tc.reason, n, tc.pinned.machines, got.fixed, got.machines)
			}
			for _, m := range tc.must {
				if !slices.ContainsFunc(got.players, func(ps []int) bool { return slices.Contains(ps, m) }) {
					t.Errorf("\n%s\nsearch(...): player %d must play, but wasn't picked", tc.reason, m)
				}
			}
		})
	}
}

// tableResponder is a responder whose response is worth a fixed number of
// points for each machine and pair (or single player) picked for it.
type tableResponder map[[3]int]float64

func (r tableResponder) respond(machines []int, players [][]int) (float64, [][]int) {
	var total float64
	for g, m := range machines {
		key := [3]int{m, players[g][0], -1}
		if len(players[g]) > 1 {
			key[2] = players[g][1]
		}
		total += r[key]
	}
	return total, make([][]int, len(machines))
}

func (r tableResponder) points(int, []int, []int) float64 { return 0 }

func TestClimb(t *testing.T) {
	cases := map[string]struct {
		reason   string
		r        tableResponder
		machines int
		players  int
		must     []bool
		start    plan
		want     plan
	}{
		"SwapPlayers": {
			reason:   "Players should swap games when each is worth more on the other's machine.",
			r:        tableResponder{{0, 0, -1}: 2, {1, 1, -1}: 2},
			machines: 2,
			players:  2,
			start:    plan{machines: []int{0, 1}, players: [][]int{{1}, {0}}},
			want:     plan{machines: []int{0, 1}, players: [][]int{{0}, {1}}, points: 4},
		},
		"SwapInMachine": {
			reason:   "A machine that isn't picked should be swapped in if it's worth more.",
			r:        tableResponder{{2, 0, -1}: 1},
			machines: 3,
			players:  1,
			start:    plan{machines: []int{0}, players: [][]int{{0}}},
			want:     plan{machines: []int{2}, players: [][]int{{0}}, points: 1},
		},
		"SwapInPlayer": {
			reason:   "A player who isn't picked should be swapped in if they're worth more.",
			r:        tableResponder{{0, 1, -1}: 1},
			machines: 1,
			players:  2,
			must:     []bool{false, false},
			start:    plan{machines: []int{0}, players: [][]int{{0}}},
			want:     plan{machines: []int{0}, players: [][]int{{1}}, points: 1},
		},
		"KeepMustPlay": {
			reason:   "A player who must play shouldn't be swapped out, even for a player worth more.",
			r:        tableResponder{{0, 1, -1}: 1},
			machines: 1,
			players:  2,
			must:     []bool{true, false},
			start:    plan{machines: []int{0}, players: [][]int{{0}}},
			want:     plan{machines: []int{0}, players: [][]int{{0}}},
		},
		"KeepPinned": {
			reason:   "Pinned games shouldn't change, even when changing them is worth more.",
			r:        tableResponder{{0, 0, -1}: 5, {2, 0, -1}: 2},
			machines: 3,
			players:  2,
			start:    plan{machines: []int{0, 1}, players: [][]int{{1}, {0}}, fixed: 1},
			want:     plan{machines: []int{0, 2}, players: [][]int{{1}, {0}}, points: 2, fixed: 1},
		},
		"SwapPairs": {
			reason:   "Pairs should swap machines when each is worth more on the other's, though swapping one player at a time isn't.",
			r:        tableResponder{{0, 2, 3}: 2, {1, 0, 1}: 2},
			machines: 2,
			players:  4,
			start:    plan{machines: []int{0, 1}, players: [][]int{{0, 1}, {2, 3}}},
			want:     plan{machines: []int{1, 0}, players: [][]int{{0, 1}, {2, 3}}, points: 4},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			must := tc.must
			if must == nil {
				must = make([]bool, tc.players)
			}
			got := climb(tc.start, tc.machines, tc.players, must, tc.r)
			if diff := cmp.Diff(tc.want, got, cmp.AllowUnexported(plan{})); diff != "" {
				t.Errorf("\n%s\nclimb(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}
