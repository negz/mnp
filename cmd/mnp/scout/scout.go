// Package scout implements the scout command.
package scout

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/output"
)

// Command scouts a team's strengths and weaknesses across machines.
type Command struct {
	Team  string `arg:""                                 help:"Team key (e.g., CRA)."`
	Venue string `help:"Filter to venue-specific stats." short:"e"`
}

// Run executes the scout command.
func (c *Command) Run(d *cache.DB) error {
	ctx := context.Background()

	store, err := d.Store(ctx)
	if err != nil {
		return err
	}

	if c.Venue != "" {
		return c.runWithVenue(ctx, store)
	}

	return c.runBasic(ctx, store)
}

// runBasic shows team performance across all machines (global stats only).
func (c *Command) runBasic(ctx context.Context, store *db.SQLiteStore) error {
	stats, err := store.GetTeamMachineStats(ctx, c.Team, "")
	if err != nil {
		return err
	}

	if len(stats) == 0 {
		fmt.Printf("No data for %s\n", c.Team)
		return nil
	}

	if err := output.Table(os.Stdout,
		[]string{"Machine", "Games", "P50", "P75", "Max", "Top Players"},
		statsToRows(stats),
	); err != nil {
		return err
	}

	printAnalysis(stats)
	return nil
}

// runWithVenue shows venue-specific stats with global fallback.
func (c *Command) runWithVenue(ctx context.Context, store *db.SQLiteStore) error {
	venueStats, err := store.GetTeamMachineStats(ctx, c.Team, c.Venue)
	if err != nil {
		return err
	}

	globalStats, err := store.GetTeamMachineStats(ctx, c.Team, "")
	if err != nil {
		return err
	}

	if len(venueStats) == 0 && len(globalStats) == 0 {
		fmt.Printf("No data for %s\n", c.Team)
		return nil
	}

	if len(venueStats) > 0 {
		fmt.Printf("At %s:\n\n", c.Venue)
		if err := output.Table(os.Stdout,
			[]string{"Machine", "Games", "P50", "P75", "Max", "Top Players"},
			statsToRows(venueStats),
		); err != nil {
			return err
		}
		fmt.Println()
	}

	venueMachineSet := make(map[string]bool)
	for _, s := range venueStats {
		venueMachineSet[s.MachineKey] = true
	}

	rows := make([][]string, 0, len(globalStats))
	for _, s := range globalStats {
		key := s.MachineKey
		if !venueMachineSet[key] {
			key += "*"
		}
		rows = append(rows, []string{
			key,
			fmt.Sprintf("%d", s.Games),
			output.FormatScore(s.P50Score),
			output.FormatScore(s.P75Score),
			output.FormatScore(float64(s.MaxScore)),
			formatTopPlayers(s.TopPlayers),
		})
	}

	fmt.Println("Global (for context):")
	fmt.Println()
	if err := output.Table(os.Stdout,
		[]string{"Machine", "Games", "P50", "P75", "Max", "Top Players"},
		rows,
	); err != nil {
		return err
	}

	for _, s := range globalStats {
		if !venueMachineSet[s.MachineKey] {
			fmt.Printf("\n*No %s data\n", c.Venue)
			break
		}
	}

	printAnalysis(venueStats)
	return nil
}

// statsToRows converts TeamMachineStats to table rows.
func statsToRows(stats []db.TeamMachineStats) [][]string {
	rows := make([][]string, len(stats))
	for i, s := range stats {
		rows[i] = []string{
			s.MachineKey,
			fmt.Sprintf("%d", s.Games),
			output.FormatScore(s.P50Score),
			output.FormatScore(s.P75Score),
			output.FormatScore(float64(s.MaxScore)),
			formatTopPlayers(s.TopPlayers),
		}
	}
	return rows
}

// formatTopPlayers formats top players as "Alice (48M), Bob (35M)".
func formatTopPlayers(players []db.TopPlayer) string {
	parts := make([]string, len(players))
	for i, p := range players {
		parts[i] = fmt.Sprintf("%s (%s)", p.Name, output.FormatScore(p.P75Score))
	}
	return strings.Join(parts, ", ")
}

// printAnalysis prints a summary of strongest and weakest machines.
func printAnalysis(stats []db.TeamMachineStats) {
	if len(stats) == 0 {
		return
	}

	fmt.Println()

	strong := make([]string, 0, 3)
	for i := range min(3, len(stats)) {
		strong = append(strong, stats[i].MachineKey)
	}
	fmt.Printf("Strongest: %s\n", strings.Join(strong, ", "))

	if len(stats) > 3 {
		weak := make([]string, 0, 3)
		for i := len(stats) - 1; i >= max(0, len(stats)-3); i-- {
			weak = append(weak, stats[i].MachineKey)
		}
		fmt.Printf("Weakest:   %s\n", strings.Join(weak, ", "))
	}
}
