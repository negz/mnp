// Package backtest implements the backtest command.
package backtest

import (
	"fmt"
	"os"

	"github.com/negz/mnp/internal/backtest"
	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/mnp"
	"github.com/negz/mnp/internal/output"
	"github.com/negz/mnp/internal/points"
	"github.com/negz/mnp/internal/recency"
)

// Command measures how well the points model predicts past games.
type Command struct {
	Archive string `help:"Path to the MNP data archive. Defaults to the cached archive."                      type:"existingdir"`
	From    int    `default:"16"                                                                              help:"First season to measure."`
	To      int    `default:"22"                                                                              help:"Last season to measure."`
	Tune    bool   `help:"Tune the model's parameters on the measured seasons, starting from the given ones."`

	HalfLife     float64   `help:"Seasons for a score to count half as much."`
	Spread       *float64  `help:"Standard deviation of log scores."`
	LeagueGames  *float64  `help:"League games of evidence a player's IPR is worth."`
	MachineGames *float64  `help:"Games of evidence the estimate before a player's own scores on a machine is worth."`
	Unplayed     *float64  `help:"Log score penalty for a machine a player hasn't played."`
	Picked       *float64  `help:"Log score bonus for the team that picked the machine."`
	Home         *float64  `help:"Log score bonus for the home team."`
	IPROffsets   []float64 `help:"Log score offset from the league's typical score for IPR 0 (unrated) to 6."         name:"ipr-offsets"`
}

// Run executes the backtest command.
func (c *Command) Run() error {
	archive := c.Archive
	if archive == "" {
		archive = cache.ArchiveDir()
	}
	seasons, err := mnp.ReadSeasons(archive)
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
	}
	if len(seasons) == 0 {
		return fmt.Errorf("no seasons in archive %s", archive)
	}

	halfLife := recency.ScoreHalfLife
	if c.HalfLife > 0 {
		halfLife = c.HalfLife
	}
	m, err := c.model()
	if err != nil {
		return err
	}

	var games []backtest.Game
	for _, g := range backtest.Replay(seasons, halfLife) {
		if g.Season >= c.From && g.Season <= c.To {
			games = append(games, g)
		}
	}

	if c.Tune {
		m = backtest.Tune(games, m)
		fmt.Printf("Tuned: %+v\n\n", m)
	}

	for _, p := range []struct {
		name    string
		predict backtest.Predictor
	}{
		{"Everyone even", backtest.Even},
		{"IPR only", backtest.IPROnly(m)},
		{"Model", m.Score},
	} {
		fmt.Printf("%s:\n\n", p.name)
		if err := output.Table(os.Stdout, headers(), rows(backtest.Evaluate(games, p.predict))); err != nil {
			return fmt.Errorf("write table: %w", err)
		}
		fmt.Println()
	}
	return nil
}

func (c *Command) model() (points.Model, error) {
	m := points.DefaultModel()
	for _, f := range []struct {
		flag  *float64
		value *float64
	}{
		{c.Spread, &m.Spread},
		{c.LeagueGames, &m.LeagueGames},
		{c.MachineGames, &m.MachineGames},
		{c.Unplayed, &m.Unplayed},
		{c.Picked, &m.Picked},
		{c.Home, &m.Home},
	} {
		if f.flag != nil {
			*f.value = *f.flag
		}
	}
	if len(c.IPROffsets) > 0 {
		if len(c.IPROffsets) != len(m.IPROffset) {
			return m, fmt.Errorf("want %d IPR offsets, got %d", len(m.IPROffset), len(c.IPROffsets))
		}
		copy(m.IPROffset[:], c.IPROffsets)
	}
	return m, nil
}

func headers() []string {
	return []string{"Slice", "Duels", "Log loss", "Brier", "Win bias", "Games", "Points MAE", "Points bias"}
}

func rows(slices []backtest.Slice) [][]string {
	out := make([][]string, 0, len(slices))
	for _, s := range slices {
		name := s.Dimension
		if s.Value != "" {
			name += " " + s.Value
		}
		out = append(out, []string{
			name,
			fmt.Sprint(s.Duels),
			fmt.Sprintf("%.4f", s.LogLoss),
			fmt.Sprintf("%.4f", s.Brier),
			fmt.Sprintf("%+.3f", s.Bias),
			fmt.Sprint(s.Games),
			fmt.Sprintf("%.3f", s.PointsMAE),
			fmt.Sprintf("%+.3f", s.PointsBias),
		})
	}
	return out
}
