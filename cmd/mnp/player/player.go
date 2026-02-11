// Package player implements the player command.
package player

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/output"
	"github.com/negz/mnp/internal/strategy/player"
)

// Command shows an individual player's stats across all machines.
type Command struct {
	Name  string `arg:""                                         help:"Player name (e.g., 'Jay Ostby')."`
	Venue string `help:"Filter to machines at a specific venue." short:"e"`
}

// Run executes the player command.
func (c *Command) Run(d *cache.DB) error {
	ctx := context.Background()
	store, err := d.SyncedStore(ctx)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	var opts []player.Option
	if c.Venue != "" {
		opts = append(opts, player.AtVenue(c.Venue))
	}

	r, err := player.Analyze(ctx, store, c.Name, opts...)
	if err != nil {
		return fmt.Errorf("look up %s: %w", c.Name, err)
	}

	if len(r.GlobalStats) == 0 {
		fmt.Printf("No data for %s\n", r.Name)
		return nil
	}

	if err := output.Table(os.Stdout, headers(), statsToRows(r.GlobalStats)); err != nil {
		return fmt.Errorf("write table: %w", err)
	}

	printFooter(r)
	return nil
}

func printFooter(r *player.Result) {
	fmt.Println()

	if r.IPR > 0 {
		fmt.Printf("IPR:  %d\n", r.IPR)
	}
	if r.Team != nil {
		fmt.Printf("Team: %s (%s)\n", r.Team.Name, r.Team.Key)
	}

	if len(r.Analysis.Strongest) > 0 {
		fmt.Printf("Strongest: %s\n", strings.Join(r.Analysis.Strongest, ", "))
	}
	if len(r.Analysis.Weakest) > 0 {
		fmt.Printf("Weakest:   %s\n", strings.Join(r.Analysis.Weakest, ", "))
	}
}

func headers() []string {
	return []string{"Machine", "Games", "P50 (vs Avg)", "P90"}
}

func statsToRows(stats []player.MachineStats) [][]string {
	rows := make([][]string, len(stats))
	for i, s := range stats {
		rows[i] = []string{
			s.MachineName,
			fmt.Sprintf("%d", s.Games),
			output.FormatP50(s.P50Score, s.LeagueP50),
			output.FormatScore(s.P90Score),
		}
	}
	return rows
}
