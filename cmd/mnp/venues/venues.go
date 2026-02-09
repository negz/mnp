// Package venues implements the venues command.
package venues

import (
	"context"
	"fmt"
	"os"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/output"
)

// Command lists all venues with their keys.
type Command struct {
	Search string `arg:"" help:"Search term (matches key or name)." optional:""`
}

// Run executes the venues command.
func (c *Command) Run(d *cache.DB) error {
	ctx := context.Background()
	store, err := d.SyncedStore(ctx)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	venues, err := store.ListVenues(ctx, c.Search)
	if err != nil {
		return fmt.Errorf("list venues: %w", err)
	}

	rows := make([][]string, len(venues))
	for i, v := range venues {
		rows[i] = []string{v.Key, v.Name}
	}

	return output.Table(os.Stdout, []string{"Key", "Name"}, rows)
}
