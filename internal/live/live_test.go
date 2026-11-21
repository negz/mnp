package live

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// A trimmed match from mondaynightpinball.com, during round 1 after the away
// team picked.
const matchJSON = `{
  "key": "mnp-24-10-DRK-TTT",
  "state": "responding",
  "round": 1,
  "home": {"key": "TTT", "name": "The Trailer Trashers", "lineup": [
    {"key": "h1", "name": "Nic Cope", "sub": false, "num_played": 0, "IPR": 5}
  ]},
  "away": {"key": "DRK", "name": "Drunken Dumplings", "lineup": [
    {"key": "a1", "name": "Matt Galbraith", "sub": true, "num_played": 1, "IPR": 5}
  ]},
  "rounds": [
    {"n": 1, "games": [{"n": 1, "machine": "SternPark", "player_1": "a1", "player_3": "a2"}]},
    {"n": 2, "games": [{"n": 1}]}
  ]
}`

var wantMatch = &Match{
	Key:   "mnp-24-10-DRK-TTT",
	State: StateResponding,
	Round: 1,
	Home: Team{Key: "TTT", Name: "The Trailer Trashers", Lineup: []Player{
		{Key: "h1", Name: "Nic Cope", IPR: 5},
	}},
	Away: Team{Key: "DRK", Name: "Drunken Dumplings", Lineup: []Player{
		{Key: "a1", Name: "Matt Galbraith", Sub: true, IPR: 5, NumPlayed: 1},
	}},
	Rounds: []Round{
		{N: 1, Games: []Game{{N: 1, Machine: "SternPark", Player1: "a1", Player3: "a2"}}},
		{N: 2, Games: []Game{{N: 1}}},
	},
}

func TestHTTPSourceMatch(t *testing.T) {
	cases := map[string]struct {
		reason  string
		status  int
		body    string
		want    *Match
		wantErr error
	}{
		"Success": {
			reason: "A match should be decoded from the site's JSON.",
			status: http.StatusOK,
			body:   matchJSON,
			want:   wantMatch,
		},
		"NotFound": {
			reason:  "A missing match should return an error.",
			status:  http.StatusNotFound,
			body:    "Not found",
			wantErr: cmpopts.AnyError,
		},
		"NotJSON": {
			reason:  "A body that isn't JSON should return an error.",
			status:  http.StatusOK,
			body:    "<html>",
			wantErr: cmpopts.AnyError,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/matches/mnp-24-10-DRK-TTT.json" {
					t.Errorf("unexpected path %q", r.URL.Path)
				}
				if r.UserAgent() != userAgent {
					t.Errorf("unexpected user agent %q", r.UserAgent())
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			got, err := NewHTTPSource(srv.URL, srv.Client()).Match(context.Background(), "mnp-24-10-DRK-TTT")
			if diff := cmp.Diff(tc.wantErr, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("\n%s\nMatch(...): -want error, +got error:\n%s", tc.reason, diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("\n%s\nMatch(...): -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestHTTPSourceRecord(t *testing.T) {
	bodies := []string{matchJSON, matchJSON, `{"key": "mnp-24-10-DRK-TTT", "state": "playing"}`}
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(bodies[n]))
		n++
	}))
	defer srv.Close()

	dir := t.TempDir()
	s := NewHTTPSource(srv.URL, srv.Client(), WithRecordDir(dir))
	for range bodies {
		if _, err := s.Match(context.Background(), "mnp-24-10-DRK-TTT"); err != nil {
			t.Fatalf("Match(...): %v", err)
		}
		// Recordings are named by the millisecond.
		time.Sleep(2 * time.Millisecond)
	}

	files, err := os.ReadDir(filepath.Join(dir, "mnp-24-10-DRK-TTT"))
	if err != nil {
		t.Fatalf("ReadDir(...): %v", err)
	}
	if diff := cmp.Diff(2, len(files)); diff != "" {
		t.Errorf("Only changed matches should be recorded: -want files, +got files:\n%s", diff)
	}
}

type fakeSource struct {
	gets int
	err  error
}

func (s *fakeSource) Match(_ context.Context, key string) (*Match, error) {
	s.gets++
	return &Match{Key: key}, s.err
}

func TestCachedSource(t *testing.T) {
	cases := map[string]struct {
		reason   string
		err      error
		after    time.Duration
		wantGets int
	}{
		"Fresh": {
			reason:   "A match got less than the TTL ago should be reused.",
			after:    29 * time.Second,
			wantGets: 1,
		},
		"Stale": {
			reason:   "A match got more than the TTL ago should be got again.",
			after:    31 * time.Second,
			wantGets: 2,
		},
		"FreshError": {
			reason:   "An error getting a match should be remembered too, so a broken site isn't hammered.",
			err:      errors.New("boom"),
			after:    29 * time.Second,
			wantGets: 1,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			src := &fakeSource{err: tc.err}
			now := time.Now()
			c := NewCachedSource(src, 30*time.Second)
			c.now = func() time.Time { return now }

			_, _ = c.Match(context.Background(), "mnp-24-10-DRK-TTT")
			now = now.Add(tc.after)
			_, _ = c.Match(context.Background(), "mnp-24-10-DRK-TTT")

			if diff := cmp.Diff(tc.wantGets, src.gets); diff != "" {
				t.Errorf("\n%s\ngets: -want, +got:\n%s", tc.reason, diff)
			}
		})
	}
}

func TestPickerResponder(t *testing.T) {
	m := &Match{Home: Team{Key: "TTT"}, Away: Team{Key: "DRK"}}
	got := [][2]string{}
	for round := 1; round <= 4; round++ {
		got = append(got, [2]string{m.Picker(round).Key, m.Responder(round).Key})
	}
	want := [][2]string{{"DRK", "TTT"}, {"TTT", "DRK"}, {"DRK", "TTT"}, {"TTT", "DRK"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("The away team should pick rounds 1 and 3, the home team 2 and 4: -want, +got:\n%s", diff)
	}
}

func TestMustPlay(t *testing.T) {
	cases := map[string]struct {
		reason string
		played int
		round  int
		want   bool
	}{
		"SatOutOnce": {
			reason: "A player who sat out a round before the last two must play both.",
			played: 1, round: 3,
			want: true,
		},
		"OnTrack": {
			reason: "A player who's played every round so far can sit out one.",
			played: 2, round: 3,
		},
		"FirstRound": {
			reason: "Nobody must play the first round.",
			played: 0, round: 1,
		},
		"SatOutFirstRound": {
			reason: "A player who sat out the first round must play the rest.",
			played: 0, round: 2,
			want: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := MustPlay(Player{NumPlayed: tc.played}, tc.round); got != tc.want {
				t.Errorf("\n%s\nMustPlay(%d played, round %d): want %t, got %t", tc.reason, tc.played, tc.round, tc.want, got)
			}
		})
	}
}

func TestCanPlay(t *testing.T) {
	cases := map[string]struct {
		reason string
		played int
		lineup int
		want   bool
	}{
		"FullLineupPlayedThree": {
			reason: "In a full lineup, a player who's played 3 games can't play a 4th.",
			played: 3, lineup: 10,
		},
		"FullLineupPlayedTwo": {
			reason: "In a full lineup, a player who's played 2 games can play a 3rd.",
			played: 2, lineup: 10,
			want: true,
		},
		"ShortLineup": {
			reason: "In a short lineup, a player who's played 3 games can play a 4th.",
			played: 3, lineup: 9,
			want: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := CanPlay(Player{NumPlayed: tc.played}, tc.lineup); got != tc.want {
				t.Errorf("\n%s\nCanPlay(%d played, lineup of %d): want %t, got %t", tc.reason, tc.played, tc.lineup, tc.want, got)
			}
		})
	}
}
