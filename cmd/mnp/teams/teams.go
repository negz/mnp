// Package teams implements the teams command.
package teams

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/output"
)

// Command lists all teams in the current season.
type Command struct {
	Search string `arg:"" help:"Search term (matches key or name)." optional:""`
}

// Run executes the teams command.
func (c *Command) Run(db *cache.DB) error {
	ctx := context.Background()

	store, err := db.Store(ctx)
	if err != nil {
		return err
	}

	query := `
		SELECT t.key, t.name, COALESCE(v.name || ' (' || v.key || ')', '') as venue
		FROM teams t
		LEFT JOIN venues v ON v.id = t.home_venue_id
		WHERE t.season_id = (SELECT MAX(season_id) FROM teams)
	`
	var args []any

	if c.Search != "" {
		query += " AND (LOWER(t.key) LIKE ? OR LOWER(t.name) LIKE ?)"
		pattern := "%" + strings.ToLower(c.Search) + "%"
		args = append(args, pattern, pattern)
	}

	query += " ORDER BY t.key"

	rows, err := store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query teams: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var results [][]string
	for rows.Next() {
		var key, name, venue string
		if err := rows.Scan(&key, &name, &venue); err != nil {
			return fmt.Errorf("scan team: %w", err)
		}
		results = append(results, []string{key, name, venue})
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate teams: %w", err)
	}

	return output.Table(os.Stdout, []string{"Key", "Name", "Venue"}, results)
}
