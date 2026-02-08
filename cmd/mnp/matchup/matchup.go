// Package matchup implements the matchup command.
package matchup

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"

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
	store, err := d.Store(ctx)
	if err != nil {
		return err
	}

	r, err := matchup.Matchup(ctx, store, c.Venue, c.Team1, c.Team2)
	if err != nil {
		return err
	}

	if len(r.Machines) == 0 {
		fmt.Printf("No machines found at %s\n", c.Venue)
		return nil
	}

	rows := make([][]string, len(r.Machines))
	for i, m := range r.Machines {
		rows[i] = []string{
			m.MachineName,
			formatScore(m.Team1P50),
			formatLikely(m.Team1Likely),
			formatScore(m.Team2P50),
			formatLikely(m.Team2Likely),
			formatEdge(m.Edge, r.Team1, r.Team2, m.Confidence),
		}
	}

	if err := output.Table(os.Stdout,
		[]string{"Machine", r.Team1 + " P50", r.Team1 + " Likely", r.Team2 + " P50", r.Team2 + " Likely", "Edge"},
		rows,
	); err != nil {
		return err
	}

	printAnalysis(r)
	return nil
}

func formatScore(score float64) string {
	if score == 0 {
		return "-"
	}
	return output.FormatScore(score)
}

func formatLikely(score float64) string {
	if score == 0 {
		return "-"
	}
	return output.FormatScore(score)
}

const (
	confHigh   = "▲"
	confMedium = "△"
	confLow    = "▼"
)

func confidenceIcon(c matchup.Confidence) string {
	switch c {
	case matchup.ConfidenceHigh:
		return confHigh
	case matchup.ConfidenceMedium:
		return confMedium
	case matchup.ConfidenceLow:
		return confLow
	}
	return confLow
}

func formatEdge(pct float64, team1, team2 string, conf matchup.Confidence) string {
	if math.IsInf(pct, 0) || pct > 1e15 || pct < -1e15 {
		if pct > 0 {
			return team1
		}
		return team2
	}
	rounded := int(math.Round(pct))
	icon := confidenceIcon(conf)
	switch {
	case rounded > 0:
		return fmt.Sprintf("%s %d%% %s", team1, rounded, icon)
	case rounded < 0:
		return fmt.Sprintf("%s %d%% %s", team2, -rounded, icon)
	default:
		return "Even"
	}
}

func printAnalysis(r *matchup.Result) {
	if len(r.Machines) == 0 {
		return
	}

	fmt.Println()
	fmt.Println(confHigh + " high confidence  " + confMedium + " medium  " + confLow + " low (based on likely players' games)")

	a := r.Analysis
	if len(a.Team1Advantages) > 0 {
		fmt.Printf("%s advantages: %s\n", r.Team1, strings.Join(a.Team1Advantages, ", "))
	}
	if len(a.Team2Advantages) > 0 {
		fmt.Printf("%s advantages: %s\n", r.Team2, strings.Join(a.Team2Advantages, ", "))
	}
	if len(a.Contested) > 0 {
		fmt.Printf("Contested: %s\n", strings.Join(a.Contested, ", "))
	}
}
