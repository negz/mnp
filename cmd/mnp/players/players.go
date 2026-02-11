// Package players implements the players command.
package players

import (
	"context"
	"fmt"
	"os"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/output"
)

// Command lists all players in the current season.
type Command struct {
	Search string `arg:"" help:"Search term (matches player name, team key, or team name)." optional:""`
}

// Run executes the players command.
func (c *Command) Run(d *cache.DB) error {
	ctx := context.Background()
	store, err := d.SyncedStore(ctx)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	players, err := store.ListPlayers(ctx, c.Search)
	if err != nil {
		return fmt.Errorf("list players: %w", err)
	}

	rows := make([][]string, len(players))
	for i, p := range players {
		rows[i] = []string{p.Name, p.TeamKey, p.Team}
	}

	return output.Table(os.Stdout, []string{"Name", "Team Key", "Team"}, rows)
}
