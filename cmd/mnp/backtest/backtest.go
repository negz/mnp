// Package backtest implements the backtest command.
package backtest

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/negz/mnp/internal/backtest"
	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/mnp"
	"github.com/negz/mnp/internal/output"
	"github.com/negz/mnp/internal/points"
	"github.com/negz/mnp/internal/recency"
)

// Command measures how well the points model predicts past games.
type Command struct {
	Archive string `help:"Path to the MNP data archive. Defaults to the cached archive." type:"existingdir"`
	From    int    `default:"16"                                                         help:"First season to measure."`
	To      int    `default:"22"                                                         help:"Last season to measure."`

	HalfLife         float64   `help:"Seasons for a score to count half as much."`
	Spread           float64   `help:"Default standard deviation of log scores."`
	PriorGames       float64   `help:"Games of evidence the IPR prior for a player's typical score is worth."`
	PriorSpreadGames float64   `help:"Games of evidence the default spread is worth."`
	IPROffsets       []float64 `help:"Log score offset from the league's typical score for IPR 1 to 6."       name:"ipr-offsets"`
}

// Run executes the backtest command.
func (c *Command) Run() error {
	archive := c.Archive
	if archive == "" {
		archive = filepath.Join(cache.Dir(), "mnp-data-archive")
	}
	seasons, err := mnp.ReadSeasons(archive)
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
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
	if c.Spread > 0 {
		m.Spread = c.Spread
	}
	if c.PriorGames > 0 {
		m.PriorGames = c.PriorGames
	}
	if c.PriorSpreadGames > 0 {
		m.PriorSpreadGames = c.PriorSpreadGames
	}
	if len(c.IPROffsets) > 0 {
		if len(c.IPROffsets) != 6 {
			return m, fmt.Errorf("want 6 IPR offsets, got %d", len(c.IPROffsets))
		}
		copy(m.IPROffset[1:], c.IPROffsets)
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
