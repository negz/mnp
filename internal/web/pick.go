package web

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/negz/mnp/internal/live"
	"github.com/negz/mnp/internal/strategy/pick"
)

// fieldMachines is the form field listing the machines that can be picked.
const fieldMachines = "m"

// lineupFields are the form fields for a team's players.
type lineupFields struct {
	Playing string // Who's playing.
	Must    string // Who must play.
	Add     string // Subs to add.
}

// pinField matches the fields for each game's machine and players.
var pinField = regexp.MustCompile(`^g\d+[mp]$`)

// subSuggestions is how many likely subs to suggest for each team.
const subSuggestions = 4

type pickData struct {
	Venue string
	Team1 string
	Team2 string

	VenueName string
	Team1Name string
	Team2Name string

	Round  int
	Rounds []pickRound // The rounds team 1 picks.
	Live   *liveData   // Set when the teams are playing each other today.

	Machines []machineChoice
	Ours     lineupChoice
	Theirs   lineupChoice

	Result *pick.Result
	Error  string

	// Pinned are the games pinned, kept in the form if there's no result to
	// show them in.
	Pinned []pick.Pin

	// Unpinned explains games that couldn't be pinned.
	Unpinned []string

	// SubNames are the rated players who aren't on a roster, so could sub.
	SubNames []string
}

// A pickRound is a round team 1 picks.
type pickRound struct {
	N       int
	Doubles bool
	URL     string // This page, for the round.
}

// A machineChoice is a machine that could be picked.
type machineChoice struct {
	Key     string
	Name    string
	Checked bool
}

// A lineupChoice is a team's players to choose from for a round.
type lineupChoice struct {
	Team    string
	Fields  lineupFields
	Players []playerChoice
	Subs    []playerChoice // Likely subs who aren't in the lineup.
}

// A playerChoice is a player who could play in a round.
type playerChoice struct {
	Name    string
	IPR     int
	Sub     bool
	Playing bool
	Must    bool
}

// handlePick suggests which machines and players team 1 should pick against
// team 2, during a match or ahead of it.
func (s *Server) handlePick(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	md, err := s.newMatchupData(ctx, q)
	if err != nil {
		s.log.Error("load matchup data", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	data := pickData{
		Venue: md.Venue, Team1: md.Team1, Team2: md.Team2,
		VenueName: md.VenueName, Team1Name: md.Team1Name, Team2Name: md.Team2Name,
	}
	if data.Venue == "" || data.Team1 == "" || data.Team2 == "" {
		data.Error = "Choose a venue and two teams on the matchup page."
		s.renderPick(w, data)
		return
	}

	home, err := s.isHome(ctx, data.Team1, data.Team2)
	if err != nil {
		s.log.Error("find match", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	data.Live = s.liveMatch(ctx, data.Team1, data.Team2, false)
	var lm *live.Match
	if data.Live != nil {
		lm = data.Live.Match
		home = lm.Home.Key == data.Team1
		// A round moves on within minutes, so don't let browsers reuse the
		// page.
		w.Header().Set("Cache-Control", "no-cache")
	}

	// The away team picks rounds 1 and 3, and the home team rounds 2 and 4.
	rounds := []int{1, 3}
	if home {
		rounds = []int{2, 4}
	}
	data.Round = nextRound(lm, data.Team1, rounds)
	if n, err := strconv.Atoi(q.Get("round")); err == nil && slices.Contains(rounds, n) {
		data.Round = n
	}
	for _, n := range rounds {
		// Games pinned in one round don't carry over to another.
		u := maps.Clone(q)
		maps.DeleteFunc(u, func(k string, _ []string) bool { return k == "pin" || k == "edit" || pinField.MatchString(k) })
		u.Set("round", strconv.Itoa(n))
		data.Rounds = append(data.Rounds, pickRound{N: n, Doubles: live.Doubles(n), URL: "/pick?" + u.Encode()})
	}

	if data.Machines, err = s.machineChoices(ctx, data.Venue, q[fieldMachines]); err != nil {
		s.log.Error("load venue machines", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if data.Ours, err = s.lineupChoice(ctx, data.Team1, lm, data.Round, q, lineupFields{Playing: "t1p", Must: "t1m", Add: "t1s"}); err != nil {
		s.log.Error("load lineup", "team", data.Team1, "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if data.Theirs, err = s.lineupChoice(ctx, data.Team2, lm, data.Round, q, lineupFields{Playing: "t2p", Must: "t2m", Add: "t2s"}); err != nil {
		s.log.Error("load lineup", "team", data.Team2, "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if data.SubNames, err = s.subNames(ctx); err != nil {
		s.log.Error("list sub names", "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	pr := pick.Round{N: data.Round, Home: home, Ours: data.Ours.playing(), Theirs: data.Theirs.playing()}
	for _, m := range data.Machines {
		if m.Checked {
			pr.Machines = append(pr.Machines, m.Key)
		}
	}
	names := make(map[string]string)
	for _, m := range data.Machines {
		names[m.Key] = m.Name
	}
	pr.Pinned, data.Unpinned = pins(q, pr, names)
	data.Pinned = pr.Pinned
	for _, l := range []struct {
		team    string
		playing []pick.Player
	}{{data.Team1, pr.Ours}, {data.Team2, pr.Theirs}} {
		if len(l.playing) > live.FullLineup {
			data.Error = fmt.Sprintf("%s has %d players, but a lineup has at most %d. Untick someone who isn't playing.", l.team, len(l.playing), live.FullLineup)
			s.renderPick(w, data)
			return
		}
	}
	result, err := pick.Analyze(ctx, s.store, pr)
	if err != nil {
		data.Error = fmt.Sprintf("Can't suggest picks: %v.", err)
	}
	data.Result = result

	s.renderPick(w, data)
}

// pins returns the games pinned in the query. Each game shown has fields for
// its machine and players, and is pinned if it's listed in the pin field. The
// edit field says which game was changed last. That game is pinned first, so if
// two games are pinned to the same machine or player it's the other that's
// unpinned. Unpinned explains each game that couldn't be pinned.
func pins(q url.Values, r pick.Round, names map[string]string) (pinned []pick.Pin, unpinned []string) {
	order := q["pin"]
	if e := q.Get("edit"); slices.Contains(order, e) {
		order = append([]string{e}, slices.DeleteFunc(slices.Clone(order), func(g string) bool { return g == e })...)
	}

	slots := 1
	if live.Doubles(r.N) {
		slots = 2
	}
	picked := make(map[string]bool)
	playing := make(map[string]bool)
	for _, g := range order {
		p := pick.Pin{Machine: q.Get("g" + g + "m"), Players: q["g"+g+"p"]}
		var problem string
		switch {
		case !slices.Contains(r.Machines, p.Machine):
			problem = "it can't be picked"
		case picked[p.Machine]:
			problem = "it's already pinned"
		case len(p.Players) != slots:
			problem = "it needs 2 players"
			if slots == 1 {
				problem = "it needs 1 player"
			}
		}
		seen := make(map[string]bool)
		for _, name := range p.Players {
			switch {
			case problem != "":
			case !slices.ContainsFunc(r.Ours, func(o pick.Player) bool { return o.Name == name }):
				problem = name + " can't play"
			case seen[name]:
				problem = name + " is picked twice"
			case playing[name]:
				problem = name + " is already pinned"
			}
			seen[name] = true
		}
		if problem != "" {
			unpinned = append(unpinned, fmt.Sprintf("Unpinned %s, since %s.", cmp.Or(names[p.Machine], p.Machine), problem))
			continue
		}
		picked[p.Machine] = true
		for name := range seen {
			playing[name] = true
		}
		pinned = append(pinned, p)
	}
	return pinned, unpinned
}

func (s *Server) renderPick(w http.ResponseWriter, data pickData) {
	if err := s.template.pick.ExecuteTemplate(w, "layout.html", data); err != nil {
		s.log.Error("render template", "err", err)
	}
}

// isHome reports whether team 1 is the home team in its next match against
// team 2 this season, or its last if they don't play again. Teams that don't
// play each other are treated as if team 1 were at home.
func (s *Server) isHome(ctx context.Context, team1, team2 string) (bool, error) {
	matches, err := s.store.ListSchedule(ctx, "")
	if err != nil {
		return false, fmt.Errorf("list schedule: %w", err)
	}
	seattle, _ := time.LoadLocation("America/Los_Angeles")
	today := time.Now().In(seattle).Format("2006-01-02")
	home := true
	for _, m := range matches {
		switch {
		case m.HomeTeamKey == team1 && m.AwayTeamKey == team2:
			home = true
		case m.HomeTeamKey == team2 && m.AwayTeamKey == team1:
			home = false
		default:
			continue
		}
		if m.Date >= today {
			break
		}
	}
	return home, nil
}

// nextRound returns the round team 1 picks next in a match, or the first round
// it picks if the match isn't under way.
func nextRound(m *live.Match, team string, rounds []int) int {
	if m == nil || !isLive(&liveData{Match: m}) {
		return rounds[0]
	}
	for _, n := range rounds {
		// Team 1 picks this round now, or will.
		if n > m.Round || (n == m.Round && m.State == live.StatePicking && m.Picker(n).Key == team) {
			return n
		}
	}
	return rounds[len(rounds)-1]
}

// machineChoices returns the machines at a venue, checked if they can be
// picked. All can be picked unless the query lists which can.
func (s *Server) machineChoices(ctx context.Context, venue string, listed []string) ([]machineChoice, error) {
	keys, err := s.store.GetVenueMachines(ctx, venue)
	if err != nil {
		return nil, fmt.Errorf("get venue machines: %w", err)
	}
	names, err := s.store.GetMachineNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("get machine names: %w", err)
	}
	machines := make([]machineChoice, 0, len(keys))
	for k := range keys {
		machines = append(machines, machineChoice{Key: k, Name: cmp.Or(names[k], k), Checked: listed == nil || slices.Contains(listed, k)})
	}
	slices.SortFunc(machines, func(a, b machineChoice) int { return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Key, b.Key)) })
	return machines, nil
}

// lineupChoice returns a team's players to choose from for a round. By default
// that's its lineup in a match under way, or else its roster. If the query
// lists who's playing, anyone else on it is added as a sub.
func (s *Server) lineupChoice(ctx context.Context, team string, m *live.Match, round int, q url.Values, fields lineupFields) (lineupChoice, error) {
	lc := lineupChoice{Team: team, Fields: fields}
	var err error
	if lc.Players, err = s.defaultLineup(ctx, team, m, round); err != nil {
		return lineupChoice{}, err
	}

	iprs, err := s.store.ListIPRs(ctx)
	if err != nil {
		return lineupChoice{}, fmt.Errorf("list IPRs: %w", err)
	}
	_, explicit := q[fields.Playing]
	listed := slices.Concat(q[fields.Playing], q[fields.Add])
	playing := make(map[string]bool)
	for i, name := range listed {
		listed[i] = canonicalName(strings.TrimSpace(name), iprs)
		playing[listed[i]] = listed[i] != ""
	}
	must := make(map[string]bool)
	for _, name := range q[fields.Must] {
		must[name] = true
	}
	for i, p := range lc.Players {
		if explicit {
			lc.Players[i].Playing = playing[p.Name]
			lc.Players[i].Must = playing[p.Name] && must[p.Name]
		}
		delete(playing, p.Name)
	}
	// Anyone else listed is a sub, in the order they were listed.
	for _, name := range listed {
		if playing[name] {
			lc.Players = append(lc.Players, playerChoice{Name: name, IPR: iprs[name], Sub: true, Playing: true, Must: must[name]})
			delete(playing, name)
		}
	}

	subs, err := s.store.ListSubs(ctx, team)
	if err != nil {
		return lineupChoice{}, fmt.Errorf("list subs: %w", err)
	}
	for _, p := range subs {
		if len(lc.Subs) < subSuggestions && !slices.ContainsFunc(lc.Players, func(c playerChoice) bool { return c.Name == p.Name }) {
			lc.Subs = append(lc.Subs, playerChoice{Name: p.Name, IPR: p.IPR, Sub: true})
		}
	}
	return lc, nil
}

// defaultLineup returns a team's lineup in a match, or its roster if it has no
// lineup yet, strongest players first.
func (s *Server) defaultLineup(ctx context.Context, team string, m *live.Match, round int) ([]playerChoice, error) {
	var lineup []live.Player
	if m != nil {
		switch team {
		case m.Home.Key:
			lineup = m.Home.Lineup
		case m.Away.Key:
			lineup = m.Away.Lineup
		}
	}
	var players []playerChoice
	if len(lineup) > 0 {
		// Players' games so far only say who must or can't play in the
		// round being picked now.
		now := round == m.Round && m.State == live.StatePicking
		for _, p := range lineup {
			players = append(players, playerChoice{
				Name:    p.Name,
				IPR:     p.IPR,
				Sub:     p.Sub,
				Playing: !now || live.CanPlay(p, len(lineup)),
				Must:    now && live.MustPlay(p, round),
			})
		}
		return players, nil
	}

	roster, err := s.store.ListPlayers(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list players: %w", err)
	}
	for _, p := range roster {
		if p.TeamKey == team {
			players = append(players, playerChoice{Name: p.Name, IPR: p.IPR, Playing: true})
		}
	}
	slices.SortStableFunc(players, func(a, b playerChoice) int { return cmp.Compare(b.IPR, a.IPR) })
	return players, nil
}

// subNames returns the names of the rated players who aren't on a roster this
// season, in order.
func (s *Server) subNames(ctx context.Context) ([]string, error) {
	iprs, err := s.store.ListIPRs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list IPRs: %w", err)
	}
	roster, err := s.store.ListPlayers(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list players: %w", err)
	}
	rostered := make(map[string]bool, len(roster))
	for _, p := range roster {
		rostered[p.Name] = true
	}
	names := make([]string, 0, len(iprs))
	for name := range iprs {
		if !rostered[name] {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names, nil
}

// playing returns the players who are playing.
func (lc lineupChoice) playing() []pick.Player {
	var out []pick.Player
	for _, p := range lc.Players {
		if p.Playing {
			out = append(out, pick.Player{Name: p.Name, IPR: p.IPR, Sub: p.Sub, Must: p.Must})
		}
	}
	return out
}

// canonicalName returns the name of the rated player whose name matches,
// ignoring case, or the name unchanged if nobody's does.
func canonicalName(name string, iprs map[string]int) string {
	if _, ok := iprs[name]; ok {
		return name
	}
	for n := range iprs {
		if strings.EqualFold(n, name) {
			return n
		}
	}
	return name
}

// playerNames joins the names of the players in a game.
func playerNames(ps []pick.Stats) string {
	if len(ps) == 0 {
		return "nobody"
	}
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name
	}
	return strings.Join(names, " & ")
}
