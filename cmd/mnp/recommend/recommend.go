// Package recommend implements the recommend command.
package recommend

import (
	"context"
	"fmt"
	"os"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/output"
)

// Command recommends which players should play a specific machine.
type Command struct {
	Team     string `arg:""                                     help:"Team key (e.g., CRA)."`
	Machine  string `arg:""                                     help:"Machine key (e.g., TZ)."`
	Venue    string `help:"Filter to venue-specific stats."     short:"e"`
	Opponent string `help:"Compare against opponent's players." name:"vs"`
}

// Run executes the recommend command.
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
	lp75 := leagueP75[c.Machine]

	if c.Opponent != "" {
		return c.runWithOpponent(ctx, store, lp75)
	}

	if c.Venue != "" {
		return c.runWithVenue(ctx, store, lp75)
	}

	return c.runBasic(ctx, store, lp75)
}

// runBasic shows player stats for a team on a machine (global stats only).
func (c *Command) runBasic(ctx context.Context, store *db.SQLiteStore, lp75 float64) error {
	stats, err := store.GetPlayerMachineStats(ctx, c.Team, c.Machine, "")
	if err != nil {
		return err
	}

	if len(stats) == 0 {
		fmt.Printf("No data for %s on %s\n", c.Team, c.Machine)
		return nil
	}

	return output.Table(os.Stdout,
		[]string{"Player", "Games", "P50", "P75 (vs Avg)", "Max"},
		statsToRows(stats, lp75),
	)
}

// runWithVenue shows venue-specific stats with global fallback.
func (c *Command) runWithVenue(ctx context.Context, store *db.SQLiteStore, lp75 float64) error {
	venueStats, err := store.GetPlayerMachineStats(ctx, c.Team, c.Machine, c.Venue)
	if err != nil {
		return err
	}

	globalStats, err := store.GetPlayerMachineStats(ctx, c.Team, c.Machine, "")
	if err != nil {
		return err
	}

	if len(venueStats) == 0 && len(globalStats) == 0 {
		fmt.Printf("No data for %s on %s\n", c.Team, c.Machine)
		return nil
	}

	if len(venueStats) > 0 {
		fmt.Printf("At %s:\n\n", c.Venue)
		if err := output.Table(os.Stdout,
			[]string{"Player", "Games", "P50", "P75 (vs Avg)", "Max"},
			statsToRows(venueStats, lp75),
		); err != nil {
			return err
		}
		fmt.Println()
	}

	venuePlayerSet := make(map[string]bool)
	for _, s := range venueStats {
		venuePlayerSet[s.Name] = true
	}

	rows := make([][]string, 0, len(globalStats))
	for _, s := range globalStats {
		name := s.Name
		if !venuePlayerSet[name] {
			name += "*"
		}
		rows = append(rows, []string{
			name,
			fmt.Sprintf("%d", s.Games),
			output.FormatScore(s.P50Score),
			output.FormatP75(s.P75Score, lp75),
			output.FormatScore(float64(s.MaxScore)),
		})
	}

	fmt.Println("Global (for context):")
	fmt.Println()
	if err := output.Table(os.Stdout,
		[]string{"Player", "Games", "P50", "P75 (vs Avg)", "Max"},
		rows,
	); err != nil {
		return err
	}

	for _, s := range globalStats {
		if !venuePlayerSet[s.Name] {
			fmt.Printf("\n*No %s data\n", c.Venue)
			break
		}
	}

	return nil
}

// runWithOpponent shows comparison against opponent's players.
func (c *Command) runWithOpponent(ctx context.Context, store *db.SQLiteStore, lp75 float64) error {
	ourStats, err := store.GetPlayerMachineStats(ctx, c.Team, c.Machine, c.Venue)
	if err != nil {
		return err
	}

	theirStats, err := store.GetPlayerMachineStats(ctx, c.Opponent, c.Machine, c.Venue)
	if err != nil {
		return err
	}

	if len(ourStats) == 0 && len(theirStats) == 0 {
		fmt.Printf("No data for %s or %s on %s\n", c.Team, c.Opponent, c.Machine)
		return nil
	}

	fmt.Printf("%s options:\n\n", c.Team)
	if len(ourStats) > 0 {
		if err := output.Table(os.Stdout,
			[]string{"Player", "Games", "P50", "P75 (vs Avg)", "Max"},
			statsToRows(ourStats, lp75),
		); err != nil {
			return err
		}
	} else {
		fmt.Println("(no data)")
	}

	fmt.Printf("\n%s likely players:\n\n", c.Opponent)
	if len(theirStats) > 0 {
		if err := output.Table(os.Stdout,
			[]string{"Player", "Games", "P50", "P75 (vs Avg)", "Max"},
			statsToRows(theirStats, lp75),
		); err != nil {
			return err
		}
	} else {
		fmt.Println("(no data)")
	}

	if len(ourStats) > 0 && len(theirStats) > 0 {
		ourBest := ourStats[0]
		theirBest := theirStats[0]
		diff := ourBest.P50Score - theirBest.P50Score

		var assessment string
		switch {
		case diff > 1_000_000:
			assessment = fmt.Sprintf("%s outscores %s's best (%s) by ~%s P50. Strong pick.",
				ourBest.Name, c.Opponent, theirBest.Name, output.FormatScore(diff))
		case diff < -1_000_000:
			assessment = fmt.Sprintf("%s's best (%s) outscores %s by ~%s P50. Weak pick.",
				c.Opponent, theirBest.Name, ourBest.Name, output.FormatScore(-diff))
		default:
			assessment = fmt.Sprintf("%s and %s's best (%s) are roughly even. Contested.",
				ourBest.Name, c.Opponent, theirBest.Name)
		}

		fmt.Printf("\nAssessment: %s\n", assessment)
	}

	return nil
}

// statsToRows converts PlayerStats to table rows.
func statsToRows(stats []db.PlayerStats, lp75 float64) [][]string {
	rows := make([][]string, len(stats))
	for i, s := range stats {
		rows[i] = []string{
			s.Name,
			fmt.Sprintf("%d", s.Games),
			output.FormatScore(s.P50Score),
			output.FormatP75(s.P75Score, lp75),
			output.FormatScore(float64(s.MaxScore)),
		}
	}
	return rows
}
