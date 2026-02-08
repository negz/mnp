// Package output provides formatted output helpers for CLI commands.
package output

import (
	"io"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
)

// Table renders a bordered ASCII table to the given writer.
func Table(w io.Writer, headers []string, rows [][]string) error {
	t := tablewriter.NewTable(w,
		tablewriter.WithHeaderAutoFormat(tw.Off),
		tablewriter.WithRowAutoWrap(tw.WrapNone),
	)

	h := make([]any, len(headers))
	for i, v := range headers {
		h[i] = v
	}
	t.Header(h...)

	if err := t.Bulk(rows); err != nil {
		return err
	}
	return t.Render()
}
