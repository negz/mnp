package web

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/live"
	"github.com/negz/mnp/internal/strategy/matchup"
	"github.com/negz/mnp/internal/strategy/pick"
)

func TestFormatEdge(t *testing.T) {
	cases := map[string]struct {
		reason string
		m      matchup.MachineMatchup
		want   string
	}{
		"Double": {
			reason: "A likely double should name the favorite.",
			m:      matchup.MachineMatchup{Team1Likely: 10, Team2Likely: 25, Favorite: "DRK", Verdict: matchup.VerdictDouble},
			want:   "DRK likely double",
		},
		"Win": {
			reason: "A likely win should name the favorite.",
			m:      matchup.MachineMatchup{Team1Likely: 12, Team2Likely: 10, Favorite: "TTT", Verdict: matchup.VerdictWin},
			want:   "TTT likely win",
		},
		"Even": {
			reason: "An even machine shouldn't name either team.",
			m:      matchup.MachineMatchup{Team1Likely: 11, Team2Likely: 10, Favorite: "TTT", Verdict: matchup.VerdictEven},
			want:   "Even",
		},
		"NotEnoughTeam2Data": {
			reason: "An unknown verdict should name the team with too little data.",
			m:      matchup.MachineMatchup{Team1Likely: 10, Team2Likely: 25, Team1Confidence: matchup.ConfidenceHigh, Favorite: "DRK"},
			want:   "Not enough DRK data",
		},
		"NotEnoughTeam1Data": {
			reason: "An unknown verdict should name team 1 when it has too little data.",
			m:      matchup.MachineMatchup{Team1Likely: 25, Team2Likely: 10, Team2Confidence: matchup.ConfidenceMedium, Favorite: "TTT"},
			want:   "Not enough TTT data",
		},
		"NotEnoughData": {
			reason: "An unknown verdict shouldn't name a team when both have too little data.",
			m:      matchup.MachineMatchup{Team1Likely: 10, Team2Likely: 10, Favorite: "TTT"},
			want:   "Not enough data",
		},
		"NeitherPlayed": {
			reason: "A machine neither team has played should say so.",
			m:      matchup.MachineMatchup{},
			want:   "Neither has played",
		},
		"Team1Only": {
			reason: "A machine only team 1 has played should say so.",
			m:      matchup.MachineMatchup{Team1Likely: 10, Team1Confidence: matchup.ConfidenceHigh, Favorite: "TTT"},
			want:   "TTT only",
		},
		"Team2Only": {
			reason: "A machine only team 2 has played should say so.",
			m:      matchup.MachineMatchup{Team2Likely: 10, Favorite: "DRK"},
			want:   "DRK only",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := formatEdge(tc.m, "TTT", "DRK")
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nformatEdge(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestFormatPicks(t *testing.T) {
	cases := map[string]struct {
		reason string
		m      matchup.MachineMatchup
		want   string
	}{
		"BothPicked": {
			reason: "Both teams' picks should be listed.",
			m:      matchup.MachineMatchup{Team1Picks: 6, Team2Picks: 11},
			want:   "Picked by TTT 6×, DRK 11×",
		},
		"OnlyTeam2Picked": {
			reason: "A team that never picked the machine should be left out.",
			m:      matchup.MachineMatchup{Team2Picks: 3},
			want:   "Picked by DRK 3×",
		},
		"NeitherPicked": {
			reason: "A machine neither team picked needs no note.",
			m:      matchup.MachineMatchup{},
			want:   "",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := formatPicks(tc.m, "TTT", "DRK")
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nformatPicks(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

// respondStore is a respond.Store with no data. If started is set, it's closed
// when work on suggestions starts, which then waits for release to be closed.
type respondStore struct {
	started chan struct{}
	release chan struct{}
}

func (s *respondStore) GetMachineNames(_ context.Context) (map[string]string, error) {
	if s.started != nil {
		close(s.started)
		<-s.release
	}
	return nil, nil
}

func (s *respondStore) GetLeagueP50(_ context.Context) (map[string]float64, error) {
	return nil, nil
}

func (s *respondStore) GetSinglePlayerMachineStats(_ context.Context, _, _ string) ([]db.PlayerMachineStats, error) {
	return nil, nil
}

func (s *respondStore) GetRelativeScore(_ context.Context, _ string, _ map[string]float64) (db.RelativeScore, error) {
	return db.RelativeScore{}, nil
}

func TestSuggestionsDontWaitOnOtherMatches(t *testing.T) {
	responding := func(key string) *live.Match {
		return &live.Match{Key: key, State: live.StateResponding, Round: 1, Rounds: []live.Round{{N: 1}}}
	}
	slow := &respondStore{started: make(chan struct{}), release: make(chan struct{})}
	s := &suggestions{}

	done := make(chan error)
	go func() {
		_, err := s.get(context.Background(), slow, responding("slow"))
		done <- err
	}()
	<-slow.started

	fast := make(chan error)
	go func() {
		_, err := s.get(context.Background(), &respondStore{}, responding("fast"))
		fast <- err
	}()
	select {
	case err := <-fast:
		if err != nil {
			t.Errorf("get(fast): %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("get(fast): still waiting on another match's suggestions")
		close(slow.release)
		<-fast
		<-done
		return
	}

	close(slow.release)
	if err := <-done; err != nil {
		t.Errorf("get(slow): %v", err)
	}
}

func TestFormatPercent(t *testing.T) {
	cases := map[string]struct {
		p    float64
		want string
	}{
		"Whole":         {p: 0.62, want: "62%"},
		"RoundsUp":      {p: 0.625, want: "63%"},
		"NearlyCertain": {p: 0.996, want: ">99%"},
		"Certain":       {p: 1, want: ">99%"},
		"Unlikely":      {p: 0.01, want: "1%"},
		"NearlyNever":   {p: 0.004, want: "<1%"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, formatPercent(tc.p)); diff != "" {
				t.Errorf("formatPercent(%v): -want, +got:\n%s", tc.p, diff)
			}
		})
	}
}

func TestPins(t *testing.T) {
	round := pick.Round{
		N:        2,
		Machines: []string{"VEN", "Jaws", "JW"},
		Ours:     []pick.Player{{Name: "Nic"}, {Name: "Rick"}, {Name: "Kyle"}},
	}
	names := map[string]string{"VEN": "Venom", "Jaws": "Jaws", "JW": "John Wick"}

	cases := map[string]struct {
		reason       string
		q            url.Values
		wantPinned   []pick.Pin
		wantUnpinned []string
	}{
		"OnlyPinned": {
			reason:     "Only games listed in the pin field should be pinned.",
			q:          url.Values{"pin": {"1"}, "g0m": {"VEN"}, "g0p": {"Nic"}, "g1m": {"Jaws"}, "g1p": {"Rick"}},
			wantPinned: []pick.Pin{{Machine: "Jaws", Players: []string{"Rick"}}},
		},
		"EditedWins": {
			reason:       "When two games are pinned to one player, the game edited last should keep them.",
			q:            url.Values{"pin": {"0", "1"}, "edit": {"1"}, "g0m": {"VEN"}, "g0p": {"Rick"}, "g1m": {"Jaws"}, "g1p": {"Rick"}},
			wantPinned:   []pick.Pin{{Machine: "Jaws", Players: []string{"Rick"}}},
			wantUnpinned: []string{"Unpinned Venom, since Rick is already pinned."},
		},
		"MachineCantBePicked": {
			reason:       "A game can't be pinned to a machine that can't be picked.",
			q:            url.Values{"pin": {"0"}, "g0m": {"TZ"}, "g0p": {"Nic"}},
			wantUnpinned: []string{"Unpinned TZ, since it can't be picked."},
		},
		"PlayerCantPlay": {
			reason:       "A game can't be pinned to a player who can't play.",
			q:            url.Values{"pin": {"0"}, "g0m": {"JW"}, "g0p": {"Blake"}},
			wantUnpinned: []string{"Unpinned John Wick, since Blake can't play."},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pinned, unpinned := pins(tc.q, round, names)
			if diff := cmp.Diff(tc.wantPinned, pinned); diff != "" {
				t.Errorf("\n%s\npins(...): -want pinned, +got:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.wantUnpinned, unpinned); diff != "" {
				t.Errorf("\n%s\npins(...): -want unpinned, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestCanonicalName(t *testing.T) {
	iprs := map[string]int{"Adam Jenkins USA": 2, "Gary Sabo": 1}
	cases := map[string]struct {
		reason string
		name   string
		want   string
	}{
		"Exact":   {reason: "A rated player's name should be unchanged.", name: "Gary Sabo", want: "Gary Sabo"},
		"Case":    {reason: "A rated player's name should match ignoring case.", name: "adam jenkins usa", want: "Adam Jenkins USA"},
		"Unrated": {reason: "A name nobody's rated under should be unchanged.", name: "New Person", want: "New Person"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, canonicalName(tc.name, iprs)); diff != "" {
				t.Errorf("\n%s\ncanonicalName(%q): -want, +got:\n%s", tc.reason, tc.name, diff)
			}
		})
	}
}

func TestNextRound(t *testing.T) {
	home := []int{2, 4}
	match := func(state live.State, round int) *live.Match {
		return &live.Match{State: state, Round: round, Home: live.Team{Key: "TTT"}, Away: live.Team{Key: "KNR"}}
	}
	cases := map[string]struct {
		reason string
		m      *live.Match
		want   int
	}{
		"NoMatch":     {reason: "Without a match the first round the team picks should be next.", want: 2},
		"NotStarted":  {reason: "Before a match starts the first round the team picks should be next.", m: match(live.StateLineups, 1), want: 2},
		"PickingNow":  {reason: "A round the team is picking now should be next.", m: match(live.StatePicking, 2), want: 2},
		"PlayingNow":  {reason: "Once the team's picks are in, the next round it picks should be next.", m: match(live.StatePlaying, 2), want: 4},
		"OtherPicker": {reason: "While the other team picks, the next round the team picks should be next.", m: match(live.StatePicking, 3), want: 4},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := nextRound(tc.m, "TTT", home); got != tc.want {
				t.Errorf("\n%s\nnextRound(...): want round %d, got %d", tc.reason, tc.want, got)
			}
		})
	}
}

func TestPickTemplate(t *testing.T) {
	lineup := func(field string) lineupChoice {
		return lineupChoice{
			Team:    "TTT",
			Fields:  lineupFields{Playing: field + "p", Must: field + "m", Add: field + "s"},
			Players: []playerChoice{{Name: "Nic", IPR: 5, Playing: true, Must: true}, {Name: "Rick", IPR: 5}, {Name: "New Person", Sub: true, Playing: true}},
			Subs:    []playerChoice{{Name: "Gary", IPR: 1, Sub: true}},
		}
	}
	cases := map[string]struct {
		reason string
		data   pickData
		want   []string
	}{
		"Result": {
			reason: "Suggestions should render with a machine and player to change for each game, and a pin for those pinned.",
			data: pickData{
				Venue: "STN", Team1: "TTT", Team2: "KNR", Round: 2,
				Rounds:   []pickRound{{N: 2, URL: "/pick?round=2"}, {N: 4, Doubles: true, URL: "/pick?round=4"}},
				Machines: []machineChoice{{Key: "VEN", Name: "Venom", Checked: true}},
				Ours:     lineup("t1"), Theirs: lineup("t2"),
				SubNames: []string{"Gary"},
				Result: &pick.Result{Round: 2, MaxPoints: 3, Games: []pick.Game{{
					MachineKey: "VEN", MachineName: "Venom", MaxPoints: 3, Pinned: true,
					Ours: []pick.Stats{{Name: "Nic"}}, Theirs: []pick.Stats{{Name: "Lee"}},
				}}},
			},
			want: []string{`name="g0m"`, `<option value="VEN" selected>`, `name="g0p"`, `<option value="Nic" selected>`, `id="pin-0" name="pin" value="0" checked`, "<mark>Pinned</mark>", `<option value="Gary">`, "IPR 1 · new"},
		},
		"Error": {
			reason: "Pins should be kept in the form when there's no result to show them in.",
			data: pickData{
				Venue: "STN", Team1: "TTT", Team2: "KNR", Round: 2,
				Rounds: []pickRound{{N: 2, URL: "/pick?round=2"}},
				Ours:   lineup("t1"), Theirs: lineup("t2"),
				Pinned: []pick.Pin{{Machine: "VEN", Players: []string{"Nic"}}},
				Error:  "Can't suggest picks.",
			},
			want: []string{`name="pin" value="0"`, `name="g0m" value="VEN"`, `name="g0p" value="Nic"`},
		},
	}
	tmpl := parseTemplates("templates/pick.html")
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var b strings.Builder
			if err := tmpl.ExecuteTemplate(&b, "layout.html", tc.data); err != nil {
				t.Fatalf("\n%s\nExecuteTemplate(...): %v", tc.reason, err)
			}
			for _, w := range tc.want {
				if !strings.Contains(b.String(), w) {
					t.Errorf("\n%s\nExecuteTemplate(...): want output to contain %s", tc.reason, w)
				}
			}
		})
	}
}
