package web

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/live"
	"github.com/negz/mnp/internal/strategy/matchup"
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
