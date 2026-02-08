// Package machines implements the machines command.
package machines

import (
	"context"
	"fmt"
	"os"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/output"
)

// Command lists all machines with their keys.
type Command struct {
	Search string `arg:"" help:"Search term (matches key or name)." optional:""`
}

// Run executes the machines command.
func (c *Command) Run(d *cache.DB) error {
	ctx := context.Background()
	store, err := d.Store(ctx)
	if err != nil {
		return err
	}

	machines, err := store.ListMachines(ctx, c.Search)
	if err != nil {
		return fmt.Errorf("list machines: %w", err)
	}

	rows := make([][]string, len(machines))
	for i, m := range machines {
		rows[i] = []string{m.Key, m.Name}
	}

	return output.Table(os.Stdout, []string{"Key", "Name"}, rows)
}
