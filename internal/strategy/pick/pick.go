// Package pick suggests which machines and players a team should pick for a
// round, given how the other team is likely to respond.
package pick

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/habit"
	"github.com/negz/mnp/internal/live"
	"github.com/negz/mnp/internal/output"
	"github.com/negz/mnp/internal/points"
)

// Rounds 1 and 4 are doubles, with 4 games of 2 players a side worth 5 points
// each. Rounds 2 and 3 are singles, with 7 games worth 3 points each.
const (
	singlesGames  = 7
	singlesPoints = 3
	doublesGames  = 4
	doublesPoints = 5
)

// Store is the set of queries needed to suggest picks.
type Store interface {
	GetLeagueP50(ctx context.Context) (map[string]float64, error)
	GetMachineNames(ctx context.Context) (map[string]string, error)
	GetSinglePlayerMachineStats(ctx context.Context, playerName, venueKey string) ([]db.PlayerMachineStats, error)
	GetRelativeScore(ctx context.Context, playerName string, league map[string]float64) (db.RelativeScore, error)
}

// A Player can play in the round.
type Player struct {
	Name string
	IPR  int
	Sub  bool
	Must bool // Whether they must play this round, e.g. because they've sat out.
}

// A Round to pick machines and players for.
type Round struct {
	N        int      // 1 to 4.
	Home     bool     // Whether the picking team is the home team.
	Venue    string   // Key of the venue the match is at.
	Machines []string // Keys of the machines that can be picked.
	Ours     []Player // The picking team's players.
	Theirs   []Player // The other team's players.
	Pinned   []Pin    // Games the picking team has decided on.
}

// A Pin is a game the picking team has decided on: a machine, and who plays
// it.
type Pin struct {
	Machine string
	Players []string // Names of the picking team's players.
}

// Stats are a player's stats on a machine.
type Stats struct {
	Name     string
	IPR      int
	Sub      bool
	Games    int // Games played on the machine, ever.
	P50Score float64
	P90Score float64
}

// A Game is a suggested pick.
type Game struct {
	MachineKey  string
	MachineName string
	Ours        []Stats // Who to pick to play it.
	Theirs      []Stats // Who the other team is likely to send.
	Points      float64 // Points our players expect to win against Theirs.
	MaxPoints   int     // Points available.
	Win         float64 // The chance our players win the game, taking most of its points.
	Pinned      bool    // Whether the picking team decided on this game.
}

// Result is a suggested set of picks for a round.
type Result struct {
	Round   int
	Doubles bool
	Games   []Game // Strongest first.

	// Points is the points our players expect to win across the round,
	// against Theirs in each game.
	Points float64

	// BestResponsePoints is the points our players expect if the other team
	// responds as well as it can, rather than as it likely will.
	BestResponsePoints float64

	MaxPoints  int      // Points available across the round.
	SittingOut []Player // Our players who aren't picked.
	Unpicked   []string // Names of the machines that aren't picked.
}

// Analyze suggests picks for a round: the machines, and the players to play
// them, that win the most points against how the other team is likely to
// respond, as captains habitually do. Should the other team respond as well as
// it can instead, the pick gives up at most 0.3 points against the safest pick.
func Analyze(ctx context.Context, s Store, r Round) (*Result, error) {
	doubles := live.Doubles(r.N)
	games, slots, maxPoints := singlesGames, 1, singlesPoints
	if doubles {
		games, slots, maxPoints = doublesGames, 2, doublesPoints
	}
	switch {
	case len(r.Machines) < games:
		return nil, fmt.Errorf("round %d needs %d machines, but only %d can be picked", r.N, games, len(r.Machines))
	case len(r.Ours) < games*slots:
		return nil, fmt.Errorf("round %d needs %d players, but only %d can play", r.N, games*slots, len(r.Ours))
	// Each player in the other team doubles the work to find its best doubles
	// response, so lineups are held to their real size.
	case len(r.Ours) > live.FullLineup:
		return nil, fmt.Errorf("a lineup has at most %d players, but yours has %d", live.FullLineup, len(r.Ours))
	case len(r.Theirs) > live.FullLineup:
		return nil, fmt.Errorf("a lineup has at most %d players, but the other team's has %d", live.FullLineup, len(r.Theirs))
	case len(r.Theirs) == 0:
		return nil, errors.New("the other team has nobody to play")
	case r.Venue == "":
		return nil, errors.New("the venue is needed to predict how the other team will respond")
	}
	pinned, err := pins(r, games, slots)
	if err != nil {
		return nil, err
	}

	names, err := s.GetMachineNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("load machine names: %w", err)
	}
	league, err := s.GetLeagueP50(ctx)
	if err != nil {
		return nil, fmt.Errorf("load league typical scores: %w", err)
	}

	ours, err := score(ctx, s, league, r.Venue, r.Machines, r.Ours, r.Home, true)
	if err != nil {
		return nil, err
	}
	theirs, err := score(ctx, s, league, r.Venue, r.Machines, r.Theirs, !r.Home, false)
	if err != nil {
		return nil, err
	}

	// Working out a doubles response takes longer, but in testing fewer starts
	// found the best doubles pick as often as more found the best singles pick.
	var best responder = newSingles(ours.scores, theirs.scores, must(r.Theirs), games, nil)
	var likely responder = newSingles(ours.scores, theirs.scores, must(r.Theirs), games, habitual(habit.Singles(), ours, theirs))
	starts := 40
	if doubles {
		best = newDoubles(ours.scores, theirs.scores, must(r.Theirs), games, nil)
		likely = newDoubles(ours.scores, theirs.scores, must(r.Theirs), games, habitual(habit.Doubles(), ours, theirs))
		starts = 12
	}

	pick, err := hedge(ctx, len(r.Machines), len(r.Ours), games, slots, must(r.Ours), pinned, starts, likely, best)
	if err != nil {
		return nil, fmt.Errorf("search picks: %w", err)
	}
	_, response := likely.respond(pick.machines, pick.players)

	result := &Result{Round: r.N, Doubles: doubles}
	result.BestResponsePoints, _ = best.respond(pick.machines, pick.players)
	picked := make(map[int]bool)
	for g, m := range pick.machines {
		picked[m] = true
		game := Game{
			MachineKey:  r.Machines[m],
			MachineName: output.MachineName(names, r.Machines[m]),
			Points:      likely.points(m, pick.players[g], response[g]),
			MaxPoints:   maxPoints,
			Pinned:      g < pick.fixed,
		}
		var o, t []points.Score
		for _, p := range pick.players[g] {
			game.Ours = append(game.Ours, ours.stats[p][m])
			o = append(o, ours.scores[p][m])
		}
		for _, p := range response[g] {
			game.Theirs = append(game.Theirs, theirs.stats[p][m])
			t = append(t, theirs.scores[p][m])
		}
		if doubles {
			game.Win = points.DoublesWin(o, t)
		} else {
			game.Win = points.Beats(o[0], pad(t)[0])
		}
		result.Games = append(result.Games, game)
		result.Points += game.Points
		result.MaxPoints += maxPoints
	}
	slices.SortStableFunc(result.Games, func(a, b Game) int { return cmp.Compare(b.Win, a.Win) })

	used := make(map[int]bool)
	for _, ps := range pick.players {
		for _, p := range ps {
			used[p] = true
		}
	}
	for i, p := range r.Ours {
		if !used[i] {
			result.SittingOut = append(result.SittingOut, p)
		}
	}
	for i, m := range r.Machines {
		if !picked[i] {
			result.Unpicked = append(result.Unpicked, output.MachineName(names, m))
		}
	}
	slices.Sort(result.Unpicked)

	return result, nil
}

// pins returns the pinned games as a plan, checking that each pins a machine
// that can be picked to the right number of players who can play, and that no
// machine or player is pinned twice.
func pins(r Round, games, slots int) (plan, error) {
	if len(r.Pinned) > games {
		return plan{}, fmt.Errorf("round %d has %d games, but %d are pinned", r.N, games, len(r.Pinned))
	}
	var p plan
	picked := make(map[int]bool)
	playing := make(map[int]bool)
	for _, pin := range r.Pinned {
		m := slices.Index(r.Machines, pin.Machine)
		switch {
		case m < 0:
			return plan{}, fmt.Errorf("%s is pinned, but can't be picked", pin.Machine)
		case picked[m]:
			return plan{}, fmt.Errorf("%s is pinned twice", pin.Machine)
		case len(pin.Players) != slots:
			return plan{}, fmt.Errorf("%s is pinned to %d players, but needs %d", pin.Machine, len(pin.Players), slots)
		}
		picked[m] = true
		var ps []int
		for _, name := range pin.Players {
			pl := slices.IndexFunc(r.Ours, func(o Player) bool { return o.Name == name })
			switch {
			case pl < 0:
				return plan{}, fmt.Errorf("%s is pinned to %s, but can't play", name, pin.Machine)
			case playing[pl]:
				return plan{}, fmt.Errorf("%s is pinned twice", name)
			}
			playing[pl] = true
			ps = append(ps, pl)
		}
		p.machines = append(p.machines, m)
		p.players = append(p.players, ps)
	}
	return p, nil
}

// pad returns scores with a missing player, who the league scores 1, if there
// are none. Singles needs someone to compare against.
func pad(s []points.Score) []points.Score {
	if len(s) == 0 {
		return []points.Score{{}}
	}
	return s
}

func must(ps []Player) []bool {
	out := make([]bool, len(ps))
	for i, p := range ps {
		out[i] = p.Must
	}
	return out
}

// scored is how a team's players are likely to score on each machine, and what
// the habit model knows about sending them to it.
type scored struct {
	stats  [][]Stats          // Indexed by player, then machine.
	scores [][]points.Score   // Indexed by player, then machine.
	habits [][]habit.Features // Indexed by player, then machine. Who they'd face isn't known yet.
}

// score works out how each player is likely to score on each machine at a
// venue.
func score(ctx context.Context, s Store, league map[string]float64, venue string, machines []string, ps []Player, home, picked bool) (scored, error) {
	out := scored{stats: make([][]Stats, len(ps)), scores: make([][]points.Score, len(ps)), habits: make([][]habit.Features, len(ps))}
	for i, p := range ps {
		ms, err := s.GetSinglePlayerMachineStats(ctx, p.Name, "")
		if err != nil {
			return scored{}, fmt.Errorf("load stats for %s: %w", p.Name, err)
		}
		vs, err := s.GetSinglePlayerMachineStats(ctx, p.Name, venue)
		if err != nil {
			return scored{}, fmt.Errorf("load stats at %s for %s: %w", venue, p.Name, err)
		}
		rel, err := s.GetRelativeScore(ctx, p.Name, league)
		if err != nil {
			return scored{}, fmt.Errorf("load relative score for %s: %w", p.Name, err)
		}
		byMachine := make(map[string]db.PlayerMachineStats, len(ms))
		for _, st := range ms {
			byMachine[st.MachineKey] = st
		}
		atVenue := make(map[string]int, len(vs))
		for _, st := range vs {
			atVenue[st.MachineKey] = st.Games
		}
		out.stats[i] = make([]Stats, len(machines))
		out.scores[i] = make([]points.Score, len(machines))
		out.habits[i] = make([]habit.Features, len(machines))
		for j, m := range machines {
			st := byMachine[m]
			out.stats[i][j] = Stats{Name: p.Name, IPR: p.IPR, Sub: p.Sub, Games: st.Games, P50Score: st.P50Score, P90Score: st.P90Score}
			out.scores[i][j] = points.DefaultModel().Score(points.Player{
				IPR:         p.IPR,
				Home:        home,
				Picked:      picked,
				Games:       st.Games,
				P50:         st.P50Score,
				LeagueGames: rel.Games,
				Relative:    rel.P50,
			}, league[m])
			out.habits[i][j] = habit.Features{IPR: p.IPR, Games: st.Games, VenueGames: atVenue[m]}
			// Stats count forfeits, scored 1. A player whose typical score
			// is a forfeit says nothing about how well they play it.
			if st.P50Score > 1 && league[m] > 0 {
				out.habits[i][j].Affinity = math.Log(st.P50Score/league[m]) - rel.P50
			}
		}
	}
	return out, nil
}
