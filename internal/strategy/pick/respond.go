package pick

import (
	"math"
	"slices"

	"github.com/negz/mnp/internal/points"
)

// A responder works out how the other team is likely to respond to a pick.
// Machines and players are indexes into the machines that can be picked and
// each team's players.
type responder interface {
	// respond returns the points our players expect against the other team's
	// response to a pick, and who they'd send to each game.
	respond(machines []int, players [][]int) (float64, [][]int)

	// points returns the points our players expect against theirs on a
	// machine.
	points(machine int, ours, theirs []int) float64
}

// A preference is what the other team would give up, as they see it, by
// sending their players against ours on a machine. They respond with whoever
// gives up least in total. In a short lineup theirs may be missing players.
type preference func(machine int, ours, theirs []int) float64

// expected returns the points our players expect against theirs on a machine.
// A team responding as well as it can prefers to hold us to the fewest. The
// league scores a missing player 1.
func expected(ours, theirs [][]points.Score) preference {
	return func(machine int, o, t []int) float64 {
		ts := make([]points.Score, len(t))
		for i, p := range t {
			ts[i] = theirs[p][machine]
		}
		if len(o) == 1 {
			return points.Singles(ours[o[0]][machine], pad(ts)[0])
		}
		return points.Doubles([]points.Score{ours[o[0]][machine], ours[o[1]][machine]}, ts)
	}
}

// mustBonus is taken off what the other team gives up by sending a player who
// must play, so they're always sent. It's far more than a round's points.
const mustBonus = 1000

// singles responds to singles picks. The other team sends a different player to
// each game, so its response is an assignment problem.
type singles struct {
	theirs int // How many players the other team has.

	// expected is the points our player expects against theirs on a machine,
	// and cost what the other team prefers to give up sending them, both
	// indexed by machine, then our player, then theirs. If the other team has
	// fewer players than games, missing players who score 1 follow its own.
	expected [][][]float64
	cost     [][][]float64
	must     []bool // Indexed like expected's players. None if too many must play.
}

// newSingles returns a responder for the other team, which sends whoever it
// prefers. With no preference it responds as well as it can.
func newSingles(ours, theirs [][]points.Score, must []bool, games int, prefer preference) *singles {
	n := max(len(theirs), games)
	s := &singles{theirs: len(theirs), expected: make([][][]float64, len(ours[0])), cost: make([][][]float64, len(ours[0])), must: make([]bool, n)}
	if countTrue(must) <= games {
		copy(s.must, must)
	}
	points := expected(ours, theirs)
	for m := range s.expected {
		s.expected[m] = make([][]float64, len(ours))
		s.cost[m] = make([][]float64, len(ours))
		for o := range ours {
			s.expected[m][o] = make([]float64, n)
			s.cost[m][o] = make([]float64, n)
			for t := range n {
				var their []int // A missing player.
				if t < len(theirs) {
					their = []int{t}
				}
				s.expected[m][o][t] = points(m, []int{o}, their)
				s.cost[m][o][t] = s.expected[m][o][t]
				if prefer != nil {
					s.cost[m][o][t] = prefer(m, []int{o}, their)
				}
			}
		}
	}
	return s
}

func (s *singles) respond(machines []int, players [][]int) (float64, [][]int) {
	cost := make([][]float64, len(machines))
	for g, m := range machines {
		cost[g] = slices.Clone(s.cost[m][players[g][0]])
		for t, must := range s.must {
			if must {
				cost[g][t] -= mustBonus
			}
		}
	}

	var total float64
	theirs := make([][]int, len(machines))
	for g, t := range hungarian(cost) {
		total += s.expected[machines[g]][players[g][0]][t]
		if t < s.theirs {
			theirs[g] = []int{t}
		}
	}
	return total, theirs
}

func (s *singles) points(machine int, ours, theirs []int) float64 {
	t := s.theirs // A missing player.
	if len(theirs) > 0 {
		t = theirs[0]
	}
	return s.expected[machine][ours[0]][t]
}

// hungarian returns the column assigned to each row that minimises the total
// cost, using each column at most once. There must be no more rows than
// columns.
func hungarian(cost [][]float64) []int { //nolint:gocognit // A standard algorithm, clearer in one piece.
	n, m := len(cost), len(cost[0])
	u := make([]float64, n+1)
	v := make([]float64, m+1)
	p := make([]int, m+1) // The row assigned to each column, from 1.
	way := make([]int, m+1)
	minv := make([]float64, m+1)
	used := make([]bool, m+1)
	for i := 1; i <= n; i++ {
		p[0] = i
		j0 := 0
		for j := range minv {
			minv[j] = math.Inf(1)
			used[j] = false
		}
		for {
			used[j0] = true
			i0, delta, j1 := p[j0], math.Inf(1), 0
			for j := 1; j <= m; j++ {
				if used[j] {
					continue
				}
				if c := cost[i0-1][j-1] - u[i0] - v[j]; c < minv[j] {
					minv[j], way[j] = c, j0
				}
				if minv[j] < delta {
					delta, j1 = minv[j], j
				}
			}
			for j := 0; j <= m; j++ {
				if used[j] {
					u[p[j]] += delta
					v[j] -= delta
				} else {
					minv[j] -= delta
				}
			}
			j0 = j1
			if p[j0] == 0 {
				break
			}
		}
		for j0 != 0 {
			j1 := way[j0]
			p[j0] = p[j1]
			j0 = j1
		}
	}
	rows := make([]int, n)
	for j := 1; j <= m; j++ {
		if p[j] != 0 {
			rows[p[j]-1] = j - 1
		}
	}
	return rows
}

// doubles responds to doubles picks. The other team pairs up its players and
// sends a pair to each game. Its response is found game by game, remembering
// the least it can give up using each set of its players so far.
type doubles struct {
	expected, prefer preference
	options          []option // Who the other team could send to a game.
	must             int      // A bit for each player who must play. None if too many must play.

	// free lists the options that could still be sent once the players in a
	// mask have been, indexed by mask.
	free [][]int

	// cached is what against returns, keyed by machine and our pair.
	cached map[[3]int]optionCosts

	// The search asks for many responses, so respond reuses these rather than
	// allocating them each time. least[g][mask] is the least the other team
	// can give up in the games before g by sending the players in mask, and
	// chose is the option sent to game g-1 to get there. reached lists the
	// masks set in each of least[g].
	least   [][]float64
	chose   [][]int
	reached [][]int
}

// An option is who the other team could send to a doubles game: a pair, or in a
// short lineup one player or nobody.
type option struct {
	players []int
	mask    int // A bit for each player.
}

// optionCosts are the points our pair expects against each option on a
// machine, and what the other team prefers to give up sending it.
type optionCosts struct {
	expected, prefer []float64
}

// newDoubles returns a responder for the other team, which sends whoever it
// prefers. With no preference it responds as well as it can.
func newDoubles(ours, theirs [][]points.Score, must []bool, games int, prefer preference) *doubles {
	size := 1 << len(theirs)
	d := &doubles{
		expected: expected(ours, theirs),
		prefer:   prefer,
		free:     make([][]int, size),
		cached:   make(map[[3]int]optionCosts),
		least:    make([][]float64, games+1),
		chose:    make([][]int, games+1),
		reached:  make([][]int, games+1),
	}
	for i := range theirs {
		for j := i + 1; j < len(theirs); j++ {
			d.options = append(d.options, option{players: []int{i, j}, mask: 1<<i | 1<<j})
		}
	}
	// Sending someone always beats sending nobody, so a lineup that can fill
	// every game always does.
	if len(theirs) < 2*games {
		for i := range theirs {
			d.options = append(d.options, option{players: []int{i}, mask: 1 << i})
		}
		d.options = append(d.options, option{})
	}
	for mask := range d.free {
		for i, opt := range d.options {
			if opt.mask&mask == 0 {
				d.free[mask] = append(d.free[mask], i)
			}
		}
	}
	if countTrue(must) <= 2*games {
		for i, m := range must {
			if m {
				d.must |= 1 << i
			}
		}
	}
	for g := range d.least {
		d.least[g] = make([]float64, size)
		d.chose[g] = make([]int, size)
		for mask := range d.least[g] {
			d.least[g][mask] = math.Inf(1)
		}
	}
	return d
}

// against returns the points our pair expects against each option on a
// machine, and what the other team prefers to give up sending it.
func (d *doubles) against(machine int, ours []int) optionCosts {
	key := [3]int{machine, ours[0], ours[1]}
	if c, ok := d.cached[key]; ok {
		return c
	}
	c := optionCosts{expected: make([]float64, len(d.options))}
	for i, opt := range d.options {
		c.expected[i] = d.expected(machine, ours, opt.players)
	}
	c.prefer = c.expected
	if d.prefer != nil {
		c.prefer = make([]float64, len(d.options))
		for i, opt := range d.options {
			c.prefer[i] = d.prefer(machine, ours, opt.players)
		}
	}
	d.cached[key] = c
	return c
}

func (d *doubles) respond(machines []int, players [][]int) (float64, [][]int) {
	for g := range d.reached {
		for _, mask := range d.reached[g] {
			d.least[g][mask] = math.Inf(1)
		}
		d.reached[g] = d.reached[g][:0]
	}
	d.least[0][0] = 0
	d.reached[0] = append(d.reached[0], 0)
	for g, m := range machines {
		prefer := d.against(m, players[g]).prefer
		next, chose := d.least[g+1], d.chose[g+1]
		for _, mask := range d.reached[g] {
			total := d.least[g][mask]
			for _, i := range d.free[mask] {
				to := mask | d.options[i].mask
				v := total + prefer[i]
				if math.IsInf(next[to], 1) {
					d.reached[g+1] = append(d.reached[g+1], to)
				}
				if v < next[to] {
					next[to], chose[to] = v, i
				}
			}
		}
	}

	g := len(machines)
	end := cheapest(d.least[g], d.reached[g], d.must)
	if end < 0 {
		// Nobody can be sent that includes everyone who must play. Ignore
		// who must play rather than suggest nothing.
		end = cheapest(d.least[g], d.reached[g], 0)
	}
	var total float64
	theirs := make([][]int, len(machines))
	for mask := end; g > 0; g-- {
		i := d.chose[g][mask]
		total += d.against(machines[g-1], players[g-1]).expected[i]
		theirs[g-1] = d.options[i].players
		mask &^= d.options[i].mask
	}
	return total, theirs
}

// cheapest returns the reached mask that gives up least and includes every
// player in must, or -1 if there isn't one.
func cheapest(least []float64, reached []int, must int) int {
	end := -1
	for _, mask := range reached {
		if mask&must == must && (end < 0 || least[mask] < least[end]) {
			end = mask
		}
	}
	return end
}

func (d *doubles) points(machine int, ours, theirs []int) float64 {
	i := slices.IndexFunc(d.options, func(o option) bool { return slices.Equal(o.players, theirs) })
	return d.against(machine, ours).expected[i]
}

func countTrue(bs []bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}
