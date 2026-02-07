// Package scout implements the scout command.
package scout

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
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

	leagueP75, err := store.GetLeagueP75(ctx)
	if err != nil {
		return err
	}

	if c.Venue != "" {
		return c.runWithVenue(ctx, store, leagueP75)
	}

	return c.runBasic(ctx, store, leagueP75)
}

// runBasic shows team performance across all machines (global stats only).
func (c *Command) runBasic(ctx context.Context, store *db.SQLiteStore, leagueP75 map[string]float64) error {
	stats, err := store.GetTeamMachineStats(ctx, c.Team, "")
	if err != nil {
		return err
	}

	if len(stats) == 0 {
		fmt.Printf("No data for %s\n", c.Team)
		return nil
	}

	if err := output.Table(os.Stdout,
		[]string{"Machine", "Games", "P50", "P75 (vs Avg)", "Max", "Top Players"},
		statsToRows(stats, leagueP75),
	); err != nil {
		return err
	}

	printAnalysis(stats, leagueP75)
	return nil
}

// runWithVenue shows venue-specific stats with global fallback.
func (c *Command) runWithVenue(ctx context.Context, store *db.SQLiteStore, leagueP75 map[string]float64) error {
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
			[]string{"Machine", "Games", "P50", "P75 (vs Avg)", "Max", "Top Players"},
			statsToRows(venueStats, leagueP75),
		); err != nil {
			return err
		}
		fmt.Println()
	}

	venueMachineSet := make(map[string]bool)
	for _, s := range venueStats {
		venueMachineSet[s.MachineKey] = true
	}

	globalByKey := make(map[string]db.TeamMachineStats)
	for _, s := range globalStats {
		globalByKey[s.MachineKey] = s
	}

	rows := make([][]string, 0, len(venueStats))
	for _, vs := range venueStats {
		gs, ok := globalByKey[vs.MachineKey]
		if !ok {
			continue
		}
		rows = append(rows, []string{
			gs.MachineKey,
			fmt.Sprintf("%d", gs.Games),
			output.FormatScore(gs.P50Score),
			output.FormatP75(gs.P75Score, leagueP75[gs.MachineKey]),
			output.FormatScore(float64(gs.MaxScore)),
			formatTopPlayers(gs.TopPlayers),
		})
	}

	if len(rows) > 0 {
		fmt.Println("Global (for context):")
		fmt.Println()
		if err := output.Table(os.Stdout,
			[]string{"Machine", "Games", "P50", "P75 (vs Avg)", "Max", "Top Players"},
			rows,
		); err != nil {
			return err
		}
	}

	printAnalysis(venueStats, leagueP75)
	return nil
}

// statsToRows converts TeamMachineStats to table rows.
func statsToRows(stats []db.TeamMachineStats, leagueP75 map[string]float64) [][]string {
	rows := make([][]string, len(stats))
	for i, s := range stats {
		rows[i] = []string{
			s.MachineKey,
			fmt.Sprintf("%d", s.Games),
			output.FormatScore(s.P50Score),
			output.FormatP75(s.P75Score, leagueP75[s.MachineKey]),
			output.FormatScore(float64(s.MaxScore)),
			formatTopPlayers(s.TopPlayers),
		}
	}
	return rows
}

// formatTopPlayers formats top players as "Alice, Bob".
func formatTopPlayers(players []db.TopPlayer) string {
	parts := make([]string, len(players))
	for i, p := range players {
		parts[i] = p.Name
	}
	return strings.Join(parts, ", ")
}

// printAnalysis prints a summary of strongest and weakest machines by relative
// strength (% above/below league P75).
func printAnalysis(stats []db.TeamMachineStats, leagueP75 map[string]float64) {
	if len(stats) == 0 {
		return
	}

	sorted := make([]db.TeamMachineStats, len(stats))
	copy(sorted, stats)
	slices.SortFunc(sorted, func(a, b db.TeamMachineStats) int {
		aRel := output.RelStr(a.P75Score, leagueP75[a.MachineKey])
		bRel := output.RelStr(b.P75Score, leagueP75[b.MachineKey])
		return cmp.Compare(bRel, aRel)
	})

	fmt.Println()

	strong := make([]string, 0, 3)
	for i := range min(3, len(sorted)) {
		strong = append(strong, sorted[i].MachineKey)
	}
	fmt.Printf("Strongest: %s\n", strings.Join(strong, ", "))

	if len(sorted) > 3 {
		weak := make([]string, 0, 3)
		for i := len(sorted) - 1; i >= max(0, len(sorted)-3); i-- {
			weak = append(weak, sorted[i].MachineKey)
		}
		fmt.Printf("Weakest:   %s\n", strings.Join(weak, ", "))
	}
}
