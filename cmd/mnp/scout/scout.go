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

	leagueP50, err := store.GetLeagueP50(ctx)
	if err != nil {
		return err
	}

	names, err := store.GetMachineNames(ctx)
	if err != nil {
		return err
	}

	if c.Venue != "" {
		return c.runWithVenue(ctx, store, leagueP50, names)
	}

	return c.runBasic(ctx, store, leagueP50, names)
}

// runBasic shows team performance across all machines (global stats only).
func (c *Command) runBasic(ctx context.Context, store *db.SQLiteStore, leagueP50 map[string]float64, names map[string]string) error {
	stats, err := store.GetTeamMachineStats(ctx, c.Team, "")
	if err != nil {
		return err
	}

	if len(stats) == 0 {
		fmt.Printf("No data for %s\n", c.Team)
		return nil
	}

	if err := output.Table(os.Stdout,
		[]string{"Machine", "Games", "P50 (vs Avg)", "P90", "Likely Players"},
		statsToRows(stats, leagueP50, names),
	); err != nil {
		return err
	}

	printAnalysis(stats, leagueP50, names)
	return nil
}

// runWithVenue shows venue-specific stats with global fallback.
func (c *Command) runWithVenue(ctx context.Context, store *db.SQLiteStore, leagueP50 map[string]float64, names map[string]string) error {
	venueMachines, err := store.GetVenueMachines(ctx, c.Venue)
	if err != nil {
		return err
	}

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
			[]string{"Machine", "Games", "P50 (vs Avg)", "P90", "Likely Players"},
			statsToRows(venueStats, leagueP50, names),
		); err != nil {
			return err
		}
		fmt.Println()
	}

	venueDataSet := make(map[string]bool)
	for _, s := range venueStats {
		venueDataSet[s.MachineKey] = true
	}

	rows := make([][]string, 0, len(venueMachines))
	hasGlobalOnly := false
	for _, gs := range globalStats {
		if !venueMachines[gs.MachineKey] {
			continue
		}
		name := machineName(names, gs.MachineKey)
		if !venueDataSet[gs.MachineKey] {
			name += "*"
			hasGlobalOnly = true
		}
		rows = append(rows, []string{
			name,
			fmt.Sprintf("%d", gs.Games),
			output.FormatP50(gs.P50Score, leagueP50[gs.MachineKey]),
			output.FormatScore(gs.P90Score),
			formatLikelyPlayers(gs.LikelyPlayers),
		})
	}

	if len(rows) > 0 {
		fmt.Println("Global (for context):")
		fmt.Println()
		if err := output.Table(os.Stdout,
			[]string{"Machine", "Games", "P50 (vs Avg)", "P90", "Likely Players"},
			rows,
		); err != nil {
			return err
		}
		if hasGlobalOnly {
			fmt.Printf("*No %s data\n", c.Venue)
		}
	}

	printAnalysis(venueStats, leagueP50, names)
	return nil
}

// statsToRows converts TeamMachineStats to table rows.
func statsToRows(stats []db.TeamMachineStats, leagueP50 map[string]float64, names map[string]string) [][]string {
	rows := make([][]string, len(stats))
	for i, s := range stats {
		rows[i] = []string{
			machineName(names, s.MachineKey),
			fmt.Sprintf("%d", s.Games),
			output.FormatP50(s.P50Score, leagueP50[s.MachineKey]),
			output.FormatScore(s.P90Score),
			formatLikelyPlayers(s.LikelyPlayers),
		}
	}
	return rows
}

// formatLikelyPlayers formats likely players as "Alice (35M), Bob (28M)".
func formatLikelyPlayers(players []db.LikelyPlayer) string {
	parts := make([]string, len(players))
	for i, p := range players {
		parts[i] = fmt.Sprintf("%s (%s)", shortName(p.Name), output.FormatScore(p.P50Score))
	}
	return strings.Join(parts, ", ")
}

func shortName(name string) string {
	first, last, ok := strings.Cut(name, " ")
	if !ok {
		return name
	}
	return first + " " + last[:1]
}

func machineName(names map[string]string, key string) string {
	if n, ok := names[key]; ok {
		return n
	}
	return key
}

// printAnalysis prints a summary of strongest and weakest machines by relative
// strength (% above/below league P50).
func printAnalysis(stats []db.TeamMachineStats, leagueP50 map[string]float64, names map[string]string) {
	if len(stats) == 0 {
		return
	}

	sorted := make([]db.TeamMachineStats, len(stats))
	copy(sorted, stats)
	slices.SortFunc(sorted, func(a, b db.TeamMachineStats) int {
		aRel := output.RelStr(a.P50Score, leagueP50[a.MachineKey])
		bRel := output.RelStr(b.P50Score, leagueP50[b.MachineKey])
		return cmp.Compare(bRel, aRel)
	})

	fmt.Println()

	strong := make([]string, 0, 3)
	for i := range min(3, len(sorted)) {
		strong = append(strong, machineName(names, sorted[i].MachineKey))
	}
	fmt.Printf("Strongest: %s\n", strings.Join(strong, ", "))

	if len(sorted) > 3 {
		weak := make([]string, 0, 3)
		for i := len(sorted) - 1; i >= max(0, len(sorted)-3); i-- {
			weak = append(weak, machineName(names, sorted[i].MachineKey))
		}
		fmt.Printf("Weakest:   %s\n", strings.Join(weak, ", "))
	}
}
