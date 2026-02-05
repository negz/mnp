// Package machines implements the machines command.
package machines

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/output"
)

// Command lists all machines with their keys.
type Command struct {
	Search string `arg:"" help:"Search term (matches key or name)." optional:""`
}

// Run executes the machines command.
func (c *Command) Run(db *cache.DB) error {
	ctx := context.Background()

	store, err := db.Store(ctx)
	if err != nil {
		return err
	}

	query := `
		SELECT m.key, m.name
		FROM machines m
		WHERE m.key IN (SELECT DISTINCT machine_key FROM games WHERE machine_key IS NOT NULL)
	`
	var args []any

	if c.Search != "" {
		query += " AND (LOWER(m.key) LIKE ? OR LOWER(m.name) LIKE ?)"
		pattern := "%" + strings.ToLower(c.Search) + "%"
		args = append(args, pattern, pattern)
	}

	query += " ORDER BY m.key"

	rows, err := store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query machines: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var results [][]string
	for rows.Next() {
		var key, name string
		if err := rows.Scan(&key, &name); err != nil {
			return fmt.Errorf("scan machine: %w", err)
		}
		results = append(results, []string{key, name})
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate machines: %w", err)
	}

	return output.Table(os.Stdout, []string{"Key", "Name"}, results)
}
