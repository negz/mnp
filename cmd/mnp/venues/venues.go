// Package venues implements the venues command.
package venues

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/output"
)

// Command lists all venues with their keys.
type Command struct {
	Search string `arg:"" help:"Search term (matches key or name)." optional:""`
}

// Run executes the venues command.
func (c *Command) Run(db *cache.DB) error {
	ctx := context.Background()

	store, err := db.Store(ctx)
	if err != nil {
		return err
	}

	query := "SELECT key, name FROM venues WHERE 1=1"
	var args []any

	if c.Search != "" {
		query += " AND (LOWER(key) LIKE ? OR LOWER(name) LIKE ?)"
		pattern := "%" + strings.ToLower(c.Search) + "%"
		args = append(args, pattern, pattern)
	}

	query += " ORDER BY key"

	rows, err := store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query venues: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var results [][]string
	for rows.Next() {
		var key, name string
		if err := rows.Scan(&key, &name); err != nil {
			return fmt.Errorf("scan venue: %w", err)
		}
		results = append(results, []string{key, name})
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate venues: %w", err)
	}

	return output.Table(os.Stdout, []string{"Key", "Name"}, results)
}
