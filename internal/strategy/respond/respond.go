// Package respond helps a team choose players to send against the machines
// and players the other team picked for a round.
package respond

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/live"
	"github.com/negz/mnp/internal/output"
	"github.com/negz/mnp/internal/points"
)

// Match bonus points need every player in a lineup to play at least 3 games.
// A full lineup of 10 has exactly enough games for that, so nobody can play a
// 4th without leaving someone else short.
const (
	minGames   = 3
	fullLineup = 10
	rounds     = 4
)

// alternates is how many alternates to suggest for each game.
const alternates = 2

// clearPick is how many points sending other players to a game must cost the
// round for the suggestion to be a clear pick. Near the best response most
// games cost the round little to change, so clear picks are rare.
const clearPick = 0.25

// Store is the set of queries needed to respond to picks.
type Store interface {
	GetLeagueP50(ctx context.Context) (map[string]float64, error)
	GetMachineNames(ctx context.Context) (map[string]string, error)
	GetSinglePlayerMachineStats(ctx context.Context, playerName, venueKey string) ([]db.PlayerMachineStats, error)
	GetRelativeScore(ctx context.Context, playerName string, league map[string]float64) (db.RelativeScore, error)
}

// A Player is in tonight's lineup, with their stats on one machine.
type Player struct {
	Name     string
	IPR      int
	Sub      bool
	Played   int // Games played so far tonight.
	Games    int // Games played on the machine, ever.
	P50Score float64
	P90Score float64
}

// An Option is a player the responding team could send to a game.
type Option struct {
	Player

	Points    float64 // Points the player expects to win, on their own.
	Suggested string  // The machine this player is suggested for, if any.
}

// A Game is one of the picking team's picks for the round.
type Game struct {
	N           int
	MachineKey  string
	MachineName string
	Theirs      []Player // The picking team's players.
	Suggested   []Option // Who to send, as part of the best response to the whole round.
	Alternates  []Option // The next best players to send, whether or not they're suggested elsewhere.
	Points      float64  // Points the suggested players expect to win.
	MaxPoints   int      // Points available.
	Win         float64  // The chance the suggested players win the game, taking most of its points.
	ClearPick   bool     // Whether sending other players would cost the round clearPick points or more.
}

// Result is the output of a respond query.
type Result struct {
	Match      string
	Round      int
	Doubles    bool
	Picker     string
	Responder  string
	Games      []Game
	Points     float64  // Points the suggested players expect to win across the round.
	MaxPoints  int      // Points available across the round.
	SittingOut []Player // Players who aren't suggested for any game.
}

// Analyze returns suggestions for the responding team in the match's current
// round. It returns nil if the round isn't waiting on a response.
func Analyze(ctx context.Context, s Store, m *live.Match) (*Result, error) {
	round := m.CurrentRound()
	if m.State != live.StateResponding || round == nil {
		return nil, nil //nolint:nilnil // No response is needed.
	}

	names, err := s.GetMachineNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("load machine names: %w", err)
	}
	league, err := s.GetLeagueP50(ctx)
	if err != nil {
		return nil, fmt.Errorf("load league typical scores: %w", err)
	}

	picker, responder := m.Picker(m.Round), m.Responder(m.Round)
	doubles := m.Round == 1 || m.Round == rounds
	slots, maxPoints := 1, 3
	if doubles {
		slots, maxPoints = 2, 5
	}

	r := &Result{
		Match:     m.Key,
		Round:     m.Round,
		Doubles:   doubles,
		Picker:    picker.Key,
		Responder: responder.Key,
	}

	st := &stats{
		store:    s,
		league:   league,
		machines: make(map[string]map[string]db.PlayerMachineStats),
		relative: make(map[string]db.RelativeScore),
	}
	picks := make([]pick, 0, len(round.Games))
	for _, g := range round.Games {
		if g.Machine == "" {
			continue
		}
		p, err := newPick(ctx, st, m, g, doubles)
		if err != nil {
			return nil, err
		}
		p.game.MachineName = output.MachineName(names, g.Machine)
		p.game.MaxPoints = maxPoints
		picks = append(picks, p)
	}

	expect := func(game int, players []int) float64 {
		return picks[game].points(players)
	}
	plan, cost := assign(len(picks), responder.Lineup, slots, m.Round, expect)

	placed := make(map[int]string)
	for i, ps := range plan {
		for _, p := range ps {
			placed[p] = picks[i].game.MachineName
		}
	}

	for i, p := range picks {
		g := p.game
		g.Points = expect(i, plan[i])
		g.Win = p.win(plan[i], doubles)
		g.ClearPick = cost[i] >= clearPick && !math.IsInf(cost[i], 1)
		for _, o := range plan[i] {
			g.Suggested = append(g.Suggested, p.options[o])
		}
		slices.SortFunc(g.Suggested, byPoints)
		g.Alternates = alternatesFor(p.options, plan[i], placed)

		r.Games = append(r.Games, g)
		r.Points += g.Points
		r.MaxPoints += g.MaxPoints
	}

	for i, lp := range responder.Lineup {
		if _, ok := placed[i]; !ok {
			r.SittingOut = append(r.SittingOut, Player{Name: lp.Name, IPR: lp.IPR, Sub: lp.Sub, Played: lp.NumPlayed})
		}
	}

	return r, nil
}

// stats loads each player's stats, once per player.
type stats struct {
	store    Store
	league   map[string]float64
	machines map[string]map[string]db.PlayerMachineStats
	relative map[string]db.RelativeScore
}

// player returns a player in tonight's lineup, with their stats on a machine,
// and their likely score on it.
func (s *stats) player(ctx context.Context, p live.Player, machine string, home, picked bool) (Player, points.Score, error) {
	if s.machines[p.Name] == nil {
		ms, err := s.store.GetSinglePlayerMachineStats(ctx, p.Name, "")
		if err != nil {
			return Player{}, points.Score{}, fmt.Errorf("load stats for %s: %w", p.Name, err)
		}
		s.machines[p.Name] = make(map[string]db.PlayerMachineStats, len(ms))
		for _, st := range ms {
			s.machines[p.Name][st.MachineKey] = st
		}
		rel, err := s.store.GetRelativeScore(ctx, p.Name, s.league)
		if err != nil {
			return Player{}, points.Score{}, fmt.Errorf("load relative score for %s: %w", p.Name, err)
		}
		s.relative[p.Name] = rel
	}
	st := s.machines[p.Name][machine]
	rel := s.relative[p.Name]
	known := points.Player{
		IPR:         p.IPR,
		Home:        home,
		Picked:      picked,
		Games:       st.Games,
		P50:         st.P50Score,
		LeagueGames: rel.Games,
		Relative:    rel.P50,
	}
	return Player{
		Name: p.Name, IPR: p.IPR, Sub: p.Sub, Played: p.NumPlayed,
		Games: st.Games, P50Score: st.P50Score, P90Score: st.P90Score,
	}, points.DefaultModel().Score(known, s.league[machine]), nil
}

// A pick is one of the picking team's picks, with every player in the
// responding team's lineup as an option for it.
type pick struct {
	game    Game
	options []Option // Indexed like the responding team's lineup.

	// expected is the points players in the responding team's lineup expect
	// to win, indexed by their positions in it. In doubles expected[i][j]
	// is for players i and j together, and expected[i][i] for player i
	// without a partner. In singles only expected[i][i] is used. Searching
	// for the best response asks for the same players many times, so
	// they're worked out once up front.
	expected [][]float64

	ours   []points.Score // Indexed like the responding team's lineup.
	theirs []points.Score // The picking team's players.
}

func newPick(ctx context.Context, st *stats, m *live.Match, g live.Game, doubles bool) (pick, error) {
	p := pick{game: Game{N: g.N, MachineKey: g.Machine}}
	pickerHome := m.Picker(m.Round) == &m.Home

	keys := []string{g.Player1}
	if doubles {
		keys = append(keys, g.Player3)
	}
	theirs := make([]points.Score, 0, len(keys))
	for _, k := range keys {
		lp, ok := m.Player(k)
		if !ok {
			continue
		}
		t, est, err := st.player(ctx, lp, g.Machine, pickerHome, true)
		if err != nil {
			return pick{}, err
		}
		p.game.Theirs = append(p.game.Theirs, t)
		theirs = append(theirs, est)
	}

	lineup := m.Responder(m.Round).Lineup
	ours := make([]points.Score, 0, len(lineup))
	for _, lp := range lineup {
		o, est, err := st.player(ctx, lp, g.Machine, !pickerHome, false)
		if err != nil {
			return pick{}, err
		}
		p.options = append(p.options, Option{Player: o, Points: alone(est, theirs, doubles)})
		ours = append(ours, est)
	}

	p.ours, p.theirs = ours, theirs
	p.expected = make([][]float64, len(ours))
	for i := range ours {
		p.expected[i] = make([]float64, len(ours))
	}
	if len(theirs) == 0 {
		return p, nil
	}
	for i := range ours {
		if !doubles {
			p.expected[i][i] = points.Singles(ours[i], theirs[0])
			continue
		}
		for j := i; j < len(ours); j++ {
			pair := []points.Score{ours[i], ours[j]}
			if j == i {
				pair = pair[:1]
			}
			p.expected[i][j] = points.Doubles(pair, theirs)
			p.expected[j][i] = p.expected[i][j]
		}
	}
	return p, nil
}

// points returns the points the given players, indexed like the responding
// team's lineup, expect to win on the pick.
func (p pick) points(players []int) float64 {
	switch len(players) {
	case 0:
		return 0
	case 1:
		return p.expected[players[0]][players[0]]
	default:
		return p.expected[players[0]][players[1]]
	}
}

// win returns the chance the given players, indexed like the responding team's
// lineup, win the pick.
func (p pick) win(players []int, doubles bool) float64 {
	if len(players) == 0 || len(p.theirs) == 0 {
		return 0
	}
	if !doubles {
		return points.Beats(p.ours[players[0]], p.theirs[0])
	}
	ours := make([]points.Score, len(players))
	for i, pl := range players {
		ours[i] = p.ours[pl]
	}
	return points.DoublesWin(ours, p.theirs)
}

// alternatesFor returns the best players for a game other than those suggested
// for it, noting where else they're suggested.
func alternatesFor(options []Option, suggested []int, placed map[int]string) []Option {
	rest := make([]Option, 0, len(options))
	for p, o := range options {
		if slices.Contains(suggested, p) || o.Games == 0 {
			continue
		}
		o.Suggested = placed[p]
		rest = append(rest, o)
	}
	slices.SortFunc(rest, byPoints)
	return rest[:min(alternates, len(rest))]
}

func byPoints(a, b Option) int {
	return cmp.Or(cmp.Compare(b.Points, a.Points), cmp.Compare(a.Name, b.Name))
}

// alone returns the points a player expects to win on their own: the whole
// game in singles, or the points for beating each player on the other team in
// doubles.
func alone(ours points.Score, theirs []points.Score, doubles bool) float64 {
	if len(theirs) == 0 {
		return 0
	}
	if !doubles {
		return points.Singles(ours, theirs[0])
	}
	var total float64
	for _, t := range theirs {
		total += points.Beats(ours, t)
	}
	return total
}

// assign returns the indexes of the lineup's players to send to each game to win
// the most points in the round. It sends each player to at most one game, and
// keeps every player on track to play at least 3 games in the match. For each
// game it also returns how many points the round would lose by sending other
// players to it, or +Inf if nobody else could go.
func assign(games int, lineup []live.Player, slots, round int, points func(game int, players []int) float64) (plan [][]int, cost []float64) {
	if games == 0 {
		return nil, nil
	}

	// After this round there are this many rounds left to catch up in.
	left := rounds - round
	must := make([]bool, len(lineup))
	cannot := make([]bool, len(lineup))
	for i, p := range lineup {
		must[i] = minGames-p.NumPlayed > left
		cannot[i] = len(lineup) >= fullLineup && p.NumPlayed >= minGames
	}

	if plan, cost := search(games, len(lineup), slots, must, cannot, points); plan != nil {
		return plan, cost
	}
	// The rules can't be met, e.g. in a short lineup. Ignore them rather than
	// suggest nothing.
	return search(games, len(lineup), slots, make([]bool, len(lineup)), make([]bool, len(lineup)), points)
}

// search tries every way of sending at most two players to each game, and
// returns the one that wins the most points while respecting must and cannot.
// It returns nil if no way does. For each game it also returns how many points
// the round would lose by sending other players to it, or +Inf if nobody else
// could go.
func search(games, players, slots int, must, cannot []bool, points func(game int, players []int) float64) ([][]int, []float64) {
	best := math.Inf(-1)
	var plan [][]int
	used := make([]bool, players)
	current := make([][]int, games)

	most := newTotals(games, players)

	// available reports whether a player could still be sent to a game.
	available := func(p int) bool { return !used[p] && !cannot[p] }

	var try func(game, from int, total float64)
	try = func(game, from int, total float64) {
		if game == games {
			if !satisfied(must, used) {
				return
			}
			most.add(current, total)
			if total > best {
				best = total
				plan = clone(current)
			}
			return
		}
		if len(current[game]) == slots {
			try(game+1, 0, total+points(game, current[game]))
			return
		}
		// Players are added to each game in index order, so each pair is
		// only tried once.
		placed := false
		for p := from; p < players; p++ {
			if !available(p) {
				continue
			}
			placed = true
			used[p] = true
			current[game] = append(current[game], p)
			try(game, p+1, total)
			current[game] = current[game][:len(current[game])-1]
			used[p] = false
		}
		if placed {
			return
		}
		// Players earlier in the lineup could fill this game, in another
		// order that's tried elsewhere.
		for p := range players {
			if available(p) {
				return
			}
		}
		// Nobody's left, e.g. in a short lineup.
		try(game+1, 0, total+points(game, current[game]))
	}
	try(0, 0, 0)

	if plan == nil {
		return nil, nil
	}
	cost := make([]float64, games)
	for g := range cost {
		cost[g] = best - most.without(g, plan[g])
	}
	return plan, cost
}

// totals records the most points a round could win sending each set of at most
// two players to each game. A set is keyed by its players' indexes, in order,
// with the number of players standing in for a missing player.
type totals struct {
	n    int // Players, plus one for a missing player.
	most [][]float64
}

func newTotals(games, players int) *totals {
	t := &totals{n: players + 1, most: make([][]float64, games)}
	for g := range t.most {
		t.most[g] = make([]float64, t.n*t.n)
		for k := range t.most[g] {
			t.most[g][k] = math.Inf(-1)
		}
	}
	return t
}

func (t *totals) key(players []int) int {
	a, b := t.n-1, t.n-1
	if len(players) > 0 {
		a = players[0]
	}
	if len(players) > 1 {
		b = players[1]
	}
	return a*t.n + b
}

// add records the points a round wins sending players to games.
func (t *totals) add(plan [][]int, total float64) {
	for g, players := range plan {
		k := t.key(players)
		t.most[g][k] = max(t.most[g][k], total)
	}
}

// without returns the most points the round could win sending players other
// than the given ones to a game, or -Inf if nobody else could go.
func (t *totals) without(game int, players []int) float64 {
	most := math.Inf(-1)
	for k, total := range t.most[game] {
		if k != t.key(players) {
			most = max(most, total)
		}
	}
	return most
}

// satisfied reports whether every player who must play is used.
func satisfied(must, used []bool) bool {
	for p := range must {
		if must[p] && !used[p] {
			return false
		}
	}
	return true
}

func clone(plan [][]int) [][]int {
	out := make([][]int, len(plan))
	for i := range plan {
		out[i] = slices.Clone(plan[i])
	}
	return out
}
