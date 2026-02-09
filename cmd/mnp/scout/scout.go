// Package scout implements the scout command.
package scout

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/output"
	"github.com/negz/mnp/internal/strategy/scout"
)

func headers() []string {
	return []string{"Machine", "Games", "P50 (vs Avg)", "P90", "Likely Players"}
}

// Command scouts a team's strengths and weaknesses across machines.
type Command struct {
	Team  string `arg:""                                         help:"Team key (e.g., CRA)."`
	Venue string `help:"Filter to machines at a specific venue." short:"e"`
}

// Run executes the scout command.
func (c *Command) Run(d *cache.DB) error {
	ctx := context.Background()
	store, err := d.SyncedStore(ctx)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	var opts []scout.Option
	if c.Venue != "" {
		opts = append(opts, scout.AtVenue(c.Venue))
	}

	r, err := scout.Analyze(ctx, store, c.Team, opts...)
	if err != nil {
		return fmt.Errorf("scout %s: %w", c.Team, err)
	}

	if len(r.GlobalStats) == 0 {
		fmt.Printf("No data for %s\n", c.Team)
		return nil
	}

	if err := output.Table(os.Stdout, headers(), statsToRows(r.GlobalStats)); err != nil {
		return fmt.Errorf("write table: %w", err)
	}

	printAnalysis(r.Analysis)
	return nil
}

func statsToRows(stats []scout.MachineStats) [][]string {
	rows := make([][]string, len(stats))
	for i, s := range stats {
		rows[i] = []string{
			s.MachineName,
			fmt.Sprintf("%d", s.Games),
			output.FormatP50(s.P50Score, s.LeagueP50),
			output.FormatScore(s.P90Score),
			formatLikelyPlayers(s.LikelyPlayers),
		}
	}
	return rows
}

func formatLikelyPlayers(players []scout.LikelyPlayer) string {
	parts := make([]string, len(players))
	for i, p := range players {
		short := p.Name
		if first, last, ok := strings.Cut(p.Name, " "); ok {
			short = first + " " + last[:1]
		}
		parts[i] = fmt.Sprintf("%s (%s)", short, output.FormatScore(p.P50Score))
	}
	return strings.Join(parts, ", ")
}

func printAnalysis(a scout.Analysis) {
	if len(a.Strongest) == 0 {
		return
	}

	fmt.Println()
	fmt.Printf("Strongest: %s\n", strings.Join(a.Strongest, ", "))
	if len(a.Weakest) > 0 {
		fmt.Printf("Weakest:   %s\n", strings.Join(a.Weakest, ", "))
	}
}
