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

	names, err := store.GetMachineNames(ctx)
	if err != nil {
		return err
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
		conf := confidence(s1.LikelyPlayers, s2.LikelyPlayers)
		rows = append(rows, machineRow{
			machine:  machineName(names, s1.MachineKey),
			p50t1:    output.FormatScore(s1.P50Score),
			likelyt1: formatLikely(l1),
			p50t2:    output.FormatScore(s2.P50Score),
			likelyt2: formatLikely(l2),
			edge:     formatEdge(e, c.Team1, c.Team2, conf),
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
			machine:  machineName(names, key),
			p50t1:    "-",
			likelyt1: "-",
			p50t2:    output.FormatScore(s2.P50Score),
			likelyt2: formatLikely(l2),
			edge:     formatEdge(e, c.Team1, c.Team2, confLow),
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

func machineName(names map[string]string, key string) string {
	if n, ok := names[key]; ok {
		return n
	}
	return key
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

const (
	confHigh   = "▲"
	confMedium = "△"
	confLow    = "▼"
)

func confidence(players1, players2 []db.LikelyPlayer) string {
	avg1 := avgGames(players1)
	avg2 := avgGames(players2)
	minAvg := min(avg1, avg2)
	switch {
	case minAvg >= 10:
		return confHigh
	case minAvg >= 3:
		return confMedium
	default:
		return confLow
	}
}

func avgGames(players []db.LikelyPlayer) float64 {
	if len(players) == 0 {
		return 0
	}
	var total int
	for _, p := range players {
		total += p.Games
	}
	return float64(total) / float64(len(players))
}

func formatEdge(pct float64, team1, team2, conf string) string {
	if math.IsInf(pct, 0) || pct > 1e15 || pct < -1e15 {
		if pct > 0 {
			return team1
		}
		return team2
	}
	rounded := int(math.Round(pct))
	switch {
	case rounded > 0:
		return fmt.Sprintf("%s %d%% %s", team1, rounded, conf)
	case rounded < 0:
		return fmt.Sprintf("%s %d%% %s", team2, -rounded, conf)
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
	fmt.Println(confHigh + " high confidence  " + confMedium + " medium  " + confLow + " low (based on likely players' games)")

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
