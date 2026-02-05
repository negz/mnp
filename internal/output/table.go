// Package output provides formatted output helpers for CLI commands.
package output

import (
	"io"

	"github.com/olekukonko/tablewriter"
)

// Table renders a bordered ASCII table to the given writer.
func Table(w io.Writer, headers []string, rows [][]string) error {
	t := tablewriter.NewWriter(w)
	t.Header(toAny(headers)...)
	if err := t.Bulk(rows); err != nil {
		return err
	}
	return t.Render()
}

// toAny converts a string slice to an any slice for tablewriter.Header.
func toAny(s []string) []any {
	result := make([]any, len(s))
	for i, v := range s {
		result[i] = v
	}
	return result
}
