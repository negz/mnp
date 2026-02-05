// Package query implements the query command.
package query

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/negz/mnp/internal/cache"
)

// Command runs SQL queries against the MNP database.
type Command struct {
	IPDBURL    string `default:"https://raw.githubusercontent.com/xantari/Ipdb.Database/refs/heads/master/Ipdb.Database/Database/ipdbdatabase.json" help:"IPDB database JSON URL."   hidden:"" name:"ipdb-url"`
	ArchiveURL string `default:"https://github.com/Invader-Zim/mnp-data-archive.git"                                                                help:"MNP archive git repo URL." hidden:""`
	Force      bool   `help:"Force full re-sync of all data."`

	SQL string `arg:"" help:"SQL query to execute."`
}

// Run executes the query command.
func (c *Command) Run(log *slog.Logger) error {
	ctx := context.Background()

	store, err := cache.EnsureDB(ctx)
	if err != nil {
		return err
	}
	defer store.Close() //nolint:errcheck // Nothing to do with error on program exit.

	err = cache.Sync(ctx, store,
		cache.WithIPDBURL(c.IPDBURL),
		cache.WithArchiveURL(c.ArchiveURL),
		cache.WithForce(c.Force),
		cache.WithLogger(log),
	)
	if err != nil {
		return err
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
