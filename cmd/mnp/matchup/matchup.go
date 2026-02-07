// Package matchup implements the matchup command.
package matchup

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/output"
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

	venueMachines, err := store.GetVenueMachines(ctx, c.Venue)
	if err != nil {
		return err
	}

	if len(venueMachines) == 0 {
		fmt.Printf("No machines found at %s\n", c.Venue)
		return nil
	}

	stats1, err := store.GetTeamMachineStats(ctx, c.Team1, "")
	if err != nil {
		return err
	}

	stats2, err := store.GetTeamMachineStats(ctx, c.Team2, "")
	if err != nil {
		return err
	}

	stats2ByMachine := make(map[string]db.TeamMachineStats, len(stats2))
	for _, s := range stats2 {
		stats2ByMachine[s.MachineKey] = s
	}

	rows := make([]machineRow, 0, len(stats1))
	for _, s1 := range stats1 {
		if !venueMachines[s1.MachineKey] {
			continue
		}

		s2 := stats2ByMachine[s1.MachineKey]
		l1 := likelyScore(s1.LikelyPlayers)
		l2 := likelyScore(s2.LikelyPlayers)

		e := edgePct(l1, l2)
		rows = append(rows, machineRow{
			machine:  s1.MachineKey,
			p50t1:    output.FormatScore(s1.P50Score),
			likelyt1: formatLikely(l1),
			p50t2:    output.FormatScore(s2.P50Score),
			likelyt2: formatLikely(l2),
			edge:     formatEdge(e, c.Team1, c.Team2),
			edgeVal:  e,
		})
		delete(stats2ByMachine, s1.MachineKey)
	}

	for key, s2 := range stats2ByMachine {
		if !venueMachines[key] {
			continue
		}

		l2 := likelyScore(s2.LikelyPlayers)

		e := edgePct(0, l2)
		rows = append(rows, machineRow{
			machine:  key,
			p50t1:    "-",
			likelyt1: "-",
			p50t2:    output.FormatScore(s2.P50Score),
			likelyt2: formatLikely(l2),
			edge:     formatEdge(e, c.Team1, c.Team2),
			edgeVal:  e,
		})
	}

	slices.SortFunc(rows, func(a, b machineRow) int {
		return cmp.Compare(b.edgeVal, a.edgeVal)
	})

	tableRows := make([][]string, len(rows))
	for i, r := range rows {
		tableRows[i] = []string{r.machine, r.p50t1, r.likelyt1, r.p50t2, r.likelyt2, r.edge}
	}

	if err := output.Table(os.Stdout,
		[]string{"Machine", c.Team1 + " P50", c.Team1 + " Likely", c.Team2 + " P50", c.Team2 + " Likely", "Edge"},
		tableRows,
	); err != nil {
		return err
	}

	printAnalysis(rows, c.Team1, c.Team2)
	return nil
}

// likelyScore returns the average P50 of the top 2 players by play count.
func likelyScore(players []db.LikelyPlayer) float64 {
	if len(players) == 0 {
		return 0
	}

	var sum float64
	for _, p := range players {
		sum += p.P50Score
	}
	return sum / float64(len(players))
}

func formatLikely(score float64) string {
	if score == 0 {
		return "-"
	}
	return output.FormatScore(score)
}

// edgePct returns the % by which the stronger likely score exceeds the weaker.
// Positive means l1 is stronger.
func edgePct(l1, l2 float64) float64 {
	lo := min(l1, l2)
	if lo == 0 {
		if l1 == l2 {
			return 0
		}
		return math.Copysign(math.MaxFloat64, l1-l2)
	}
	return (l1 - l2) / lo * 100
}

func formatEdge(pct float64, team1, team2 string) string {
	if math.IsInf(pct, 0) || pct > 1e15 || pct < -1e15 {
		if pct > 0 {
			return team1
		}
		return team2
	}
	rounded := int(math.Round(pct))
	switch {
	case rounded > 0:
		return fmt.Sprintf("%s %d%%", team1, rounded)
	case rounded < 0:
		return fmt.Sprintf("%s %d%%", team2, -rounded)
	default:
		return "Even"
	}
}

type machineRow struct {
	machine  string
	p50t1    string
	likelyt1 string
	p50t2    string
	likelyt2 string
	edge     string
	edgeVal  float64
}

func printAnalysis(rows []machineRow, team1, team2 string) {
	if len(rows) == 0 {
		return
	}

	fmt.Println()

	var adv1, adv2, contested []string
	for _, r := range rows {
		switch {
		case r.edgeVal > 0:
			adv1 = append(adv1, r.machine)
		case r.edgeVal < 0:
			adv2 = append(adv2, r.machine)
		default:
			contested = append(contested, r.machine)
		}
	}

	if len(adv1) > 0 {
		fmt.Printf("%s advantages: %s\n", team1, strings.Join(adv1, ", "))
	}
	if len(adv2) > 0 {
		fmt.Printf("%s advantages: %s\n", team2, strings.Join(adv2, ", "))
	}
	if len(contested) > 0 {
		fmt.Printf("Contested: %s\n", strings.Join(contested, ", "))
	}
}
