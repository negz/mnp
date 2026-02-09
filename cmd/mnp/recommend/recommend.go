// Package recommend implements the recommend command.
package recommend

import (
	"context"
	"fmt"
	"os"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/output"
	"github.com/negz/mnp/internal/strategy/recommend"
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
	store, err := d.SyncedStore(ctx)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	var opts []recommend.Option
	if c.Venue != "" {
		opts = append(opts, recommend.AtVenue(c.Venue))
	}
	if c.Opponent != "" {
		opts = append(opts, recommend.VsOpponent(c.Opponent))
	}

	r, err := recommend.Analyze(ctx, store, c.Team, c.Machine, opts...)
	if err != nil {
		return fmt.Errorf("recommend %s on %s: %w", c.Team, c.Machine, err)
	}

	if r.Opponent != "" {
		return printOpponent(r)
	}

	if r.Venue != "" {
		return printVenue(r)
	}

	return printBasic(r)
}

func printBasic(r *recommend.Result) error {
	if len(r.GlobalStats) == 0 {
		fmt.Printf("No data for %s on %s\n", r.Team, r.Machine)
		return nil
	}

	return output.Table(os.Stdout, headers(), statsToRows(r.GlobalStats))
}

func printVenue(r *recommend.Result) error {
	if len(r.VenueStats) == 0 && len(r.GlobalStats) == 0 {
		fmt.Printf("No data for %s on %s\n", r.Team, r.Machine)
		return nil
	}

	if len(r.VenueStats) > 0 {
		fmt.Printf("At %s:\n\n", r.Venue)
		if err := output.Table(os.Stdout, headers(), statsToRows(r.VenueStats)); err != nil {
			return fmt.Errorf("write table: %w", err)
		}
		fmt.Println()
	}

	fmt.Println("Global (for context):")
	fmt.Println()
	if err := output.Table(os.Stdout, headers(), statsToRows(r.GlobalStats)); err != nil {
		return fmt.Errorf("write table: %w", err)
	}

	for _, s := range r.GlobalStats {
		if s.NoVenueData {
			fmt.Printf("\n*No %s data\n", r.Venue)
			break
		}
	}

	return nil
}

func printOpponent(r *recommend.Result) error {
	if len(r.GlobalStats) == 0 && len(r.OpponentStats) == 0 {
		fmt.Printf("No data for %s or %s on %s\n", r.Team, r.Opponent, r.Machine)
		return nil
	}

	fmt.Printf("%s options:\n\n", r.Team)
	if len(r.GlobalStats) > 0 {
		if err := output.Table(os.Stdout, headers(), statsToRows(r.GlobalStats)); err != nil {
			return fmt.Errorf("write table: %w", err)
		}
	} else {
		fmt.Println("(no data)")
	}

	fmt.Printf("\n%s likely players:\n\n", r.Opponent)
	if len(r.OpponentStats) > 0 {
		if err := output.Table(os.Stdout, headers(), statsToRows(r.OpponentStats)); err != nil {
			return fmt.Errorf("write table: %w", err)
		}
	} else {
		fmt.Println("(no data)")
	}

	if a := r.Assessment; a != nil {
		fmt.Printf("\nAssessment: %s\n", formatAssessment(r))
	}

	return nil
}

func formatAssessment(r *recommend.Result) string {
	a := r.Assessment
	switch a.Verdict {
	case recommend.VerdictStrong:
		return fmt.Sprintf("%s outscores %s's best (%s) by ~%s P50. Strong pick.",
			a.OurBest, r.Opponent, a.TheirBest, output.FormatScore(a.Diff))
	case recommend.VerdictWeak:
		return fmt.Sprintf("%s's best (%s) outscores %s by ~%s P50. Weak pick.",
			r.Opponent, a.TheirBest, a.OurBest, output.FormatScore(-a.Diff))
	case recommend.VerdictContested:
		return fmt.Sprintf("%s and %s's best (%s) are roughly even. Contested.",
			a.OurBest, r.Opponent, a.TheirBest)
	}
	return ""
}

func headers() []string {
	return []string{"Player", "Games", "P50 (vs Avg)", "P90"}
}

func statsToRows(stats []recommend.PlayerStats) [][]string {
	rows := make([][]string, len(stats))
	for i, s := range stats {
		name := s.Name
		if s.NoVenueData {
			name += "*"
		}
		rows[i] = []string{
			name,
			fmt.Sprintf("%d", s.Games),
			output.FormatP50(s.P50Score, s.LeagueP50),
			output.FormatScore(s.P90Score),
		}
	}
	return rows
}
