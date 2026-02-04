// Package query implements the query command.
package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	_ "modernc.org/sqlite" // SQL driver registration.
)

// Command runs SQL queries against the MNP database.
type Command struct {
	DBPath string `default:"mnp.db" help:"Path to SQLite database."`

	SQL string `arg:"" help:"SQL query to execute."`
}

// Run executes the query command.
func (c *Command) Run() error {
	ctx := context.Background()
	dbPath := expandPath(c.DBPath)

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close() //nolint:errcheck // Nothing to do with error on program exit.

	rows, err := db.QueryContext(ctx, c.SQL)
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

func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}
