// Package matchup implements the matchup command.
package matchup

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/output"
	"github.com/negz/mnp/internal/strategy/matchup"
)

// Command compares two teams head-to-head at a venue.
type Command struct {
	Venue string `arg:"" help:"Venue key (e.g., ANC)."`
	Team1 string `arg:"" help:"First team key (e.g., CRA)."`
	Team2 string `arg:"" help:"Second team key (e.g., PYC)."`
}

// Run executes the matchup command.
func (c *Command) Run(d *cache.DB) error {
	ctx := context.Background()
	store, err := d.SyncedStore(ctx)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	r, err := matchup.Analyze(ctx, store, c.Venue, c.Team1, c.Team2)
	if err != nil {
		return fmt.Errorf("matchup %s vs %s: %w", c.Team1, c.Team2, err)
	}

	if len(r.Machines) == 0 {
		fmt.Printf("No machines found at %s\n", c.Venue)
		return nil
	}

	rows := make([][]string, len(r.Machines))
	for i, m := range r.Machines {
		rows[i] = []string{
			m.MachineName,
			formatScore(m.Team1Likely),
			formatScore(m.Team2Likely),
			formatPicks(m.Team1Picks),
			formatPicks(m.Team2Picks),
			formatEdge(m, r.Team1, r.Team2),
		}
	}

	if err := output.Table(os.Stdout,
		[]string{"Machine", r.Team1 + " Likely", r.Team2 + " Likely", r.Team1 + " Picks", r.Team2 + " Picks", "Edge"},
		rows,
	); err != nil {
		return fmt.Errorf("write table: %w", err)
	}

	fmt.Println()
	fmt.Println("Likely: average P50 of each team's two most frequent players on the machine.")
	fmt.Println("Picks: times the team's current players picked the machine, for any team, at any venue.")
	return nil
}

func formatScore(score float64) string {
	if score == 0 {
		return "-"
	}
	return output.FormatScore(score)
}

func formatPicks(n int) string {
	if n == 0 {
		return "-"
	}
	return strconv.Itoa(n)
}

func formatEdge(m matchup.MachineMatchup, team1, team2 string) string {
	switch {
	case m.Favorite == "":
		return "Neither has played"
	case m.Team2Likely == 0:
		return team1 + " only"
	case m.Team1Likely == 0:
		return team2 + " only"
	}
	switch m.Verdict {
	case matchup.VerdictDouble:
		return m.Favorite + " likely double"
	case matchup.VerdictWin:
		return m.Favorite + " likely win"
	case matchup.VerdictEven:
		return "Even"
	case matchup.VerdictUnknown:
	}
	switch {
	case m.Team1Confidence == matchup.ConfidenceLow && m.Team2Confidence == matchup.ConfidenceLow:
		return "Not enough data"
	case m.Team1Confidence == matchup.ConfidenceLow:
		return "Not enough " + team1 + " data"
	default:
		return "Not enough " + team2 + " data"
	}
}
