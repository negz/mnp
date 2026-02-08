// Package player implements the player command.
package player

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

const minGamesForAnalysis = 3

// Command shows an individual player's stats across all machines.
type Command struct {
	Name  string `arg:""                                 help:"Player name (e.g., 'Jay Ostby')."`
	Venue string `help:"Filter to venue-specific stats." short:"e"`
}

// Run executes the player command.
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

	stats, err := store.GetSinglePlayerMachineStats(ctx, c.Name, "")
	if err != nil {
		return err
	}

	if len(stats) == 0 {
		fmt.Printf("No data for %s\n", c.Name)
		return nil
	}

	if err := output.Table(os.Stdout,
		[]string{"Machine", "Games", "P50 (vs Avg)", "P90"},
		statsToRows(stats, leagueP50, names),
	); err != nil {
		return err
	}

	c.printFooter(ctx, store, stats, leagueP50, names)
	return nil
}

func (c *Command) runWithVenue(ctx context.Context, store *db.SQLiteStore, leagueP50 map[string]float64, names map[string]string) error {
	venueStats, err := store.GetSinglePlayerMachineStats(ctx, c.Name, c.Venue)
	if err != nil {
		return err
	}

	venueMachines, err := store.GetVenueMachines(ctx, c.Venue)
	if err != nil {
		return err
	}

	globalStats, err := store.GetSinglePlayerMachineStats(ctx, c.Name, "")
	if err != nil {
		return err
	}

	if len(venueStats) == 0 && len(globalStats) == 0 {
		fmt.Printf("No data for %s\n", c.Name)
		return nil
	}

	filtered := make([]db.PlayerMachineStats, 0, len(venueStats))
	for _, s := range venueStats {
		if venueMachines[s.MachineKey] {
			filtered = append(filtered, s)
		}
	}
	venueStats = filtered

	if len(venueStats) > 0 {
		fmt.Printf("At %s:\n\n", c.Venue)
		if err := output.Table(os.Stdout,
			[]string{"Machine", "Games", "P50 (vs Avg)", "P90"},
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

	hasGlobalOnly := false
	rows := make([][]string, 0, len(venueMachines))
	for _, s := range globalStats {
		if !venueMachines[s.MachineKey] {
			continue
		}
		name := output.MachineName(names, s.MachineKey)
		if !venueDataSet[s.MachineKey] {
			name += "*"
			hasGlobalOnly = true
		}
		rows = append(rows, []string{
			name,
			fmt.Sprintf("%d", s.Games),
			output.FormatP50(s.P50Score, leagueP50[s.MachineKey]),
			output.FormatScore(s.P90Score),
		})
	}

	if len(rows) > 0 {
		fmt.Println("Global (for context):")
		fmt.Println()
		if err := output.Table(os.Stdout,
			[]string{"Machine", "Games", "P50 (vs Avg)", "P90"},
			rows,
		); err != nil {
			return err
		}
		if hasGlobalOnly {
			fmt.Printf("*No %s data\n", c.Venue)
		}
	}

	c.printFooter(ctx, store, globalStats, leagueP50, names)
	return nil
}

func (c *Command) printFooter(ctx context.Context, store *db.SQLiteStore, stats []db.PlayerMachineStats, leagueP50 map[string]float64, names map[string]string) {
	fmt.Println()

	pt, err := store.GetPlayerTeam(ctx, c.Name)
	if err == nil {
		fmt.Printf("Team: %s (%s)\n", pt.TeamName, pt.TeamKey)
	}

	sorted := make([]db.PlayerMachineStats, 0, len(stats))
	for _, s := range stats {
		if s.Games >= minGamesForAnalysis {
			sorted = append(sorted, s)
		}
	}
	slices.SortFunc(sorted, func(a, b db.PlayerMachineStats) int {
		aRel := output.RelStr(a.P50Score, leagueP50[a.MachineKey])
		bRel := output.RelStr(b.P50Score, leagueP50[b.MachineKey])
		return cmp.Compare(bRel, aRel)
	})

	if len(sorted) == 0 {
		return
	}

	strong := make([]string, 0, 3)
	for i := range min(3, len(sorted)) {
		strong = append(strong, output.MachineName(names, sorted[i].MachineKey))
	}
	fmt.Printf("Strongest: %s\n", strings.Join(strong, ", "))

	if len(sorted) > 3 {
		weak := make([]string, 0, 3)
		for i := len(sorted) - 1; i >= max(0, len(sorted)-3); i-- {
			weak = append(weak, output.MachineName(names, sorted[i].MachineKey))
		}
		fmt.Printf("Weakest:   %s\n", strings.Join(weak, ", "))
	}
}

func statsToRows(stats []db.PlayerMachineStats, leagueP50 map[string]float64, names map[string]string) [][]string {
	rows := make([][]string, len(stats))
	for i, s := range stats {
		rows[i] = []string{
			output.MachineName(names, s.MachineKey),
			fmt.Sprintf("%d", s.Games),
			output.FormatP50(s.P50Score, leagueP50[s.MachineKey]),
			output.FormatScore(s.P90Score),
		}
	}
	return rows
}
