// Package query implements the query command.
package query

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/negz/mnp/internal/cache"
)

// Command runs SQL queries against the MNP database.
type Command struct {
	SQL string `arg:"" help:"SQL query to execute."`
}

// Run executes the query command.
func (c *Command) Run(db *cache.DB) error {
	ctx := context.Background()
	store, err := db.Store(ctx)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	rows, err := store.DB().QueryContext(ctx, c.SQL)
	if err != nil {
		return fmt.Errorf("query: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Nothing to do with error on program exit.

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Errorf("get columns: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	// Print header
	fmt.Fprintln(w, strings.Join(cols, "\t")) //nolint:errcheck // Errors surface in Flush.

	// Print rows
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("scan row: %w", err)
		}

		strs := make([]string, len(cols))
		for i, v := range values {
			if v == nil {
				strs[i] = ""
			} else {
				strs[i] = fmt.Sprintf("%v", v)
			}
		}
		fmt.Fprintln(w, strings.Join(strs, "\t")) //nolint:errcheck // Errors surface in Flush.
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate rows: %w", err)
	}

	return w.Flush()
}
