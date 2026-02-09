// Package teams implements the teams command.
package teams

import (
	"context"
	"fmt"
	"os"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/output"
)

// Command lists all teams in the current season.
type Command struct {
	Search string `arg:"" help:"Search term (matches key or name)." optional:""`
}

// Run executes the teams command.
func (c *Command) Run(d *cache.DB) error {
	ctx := context.Background()
	store, err := d.SyncedStore(ctx)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	teams, err := store.ListTeams(ctx, c.Search)
	if err != nil {
		return fmt.Errorf("list teams: %w", err)
	}

	rows := make([][]string, len(teams))
	for i, t := range teams {
		rows[i] = []string{t.Key, t.Name, t.Venue}
	}

	return output.Table(os.Stdout, []string{"Key", "Name", "Venue"}, rows)
}
