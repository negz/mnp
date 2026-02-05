// Package main implements the mnp CLI for querying Monday Night Pinball data.
package main

import (
	"log/slog"
	"os"

	"github.com/alecthomas/kong"

	"github.com/negz/mnp/cmd/mnp/query"
	"github.com/negz/mnp/cmd/mnp/schema"
)

type cli struct {
	Verbose bool `help:"Print sync progress." short:"v"`

	Query  query.Command  `cmd:"" help:"Run a SQL query against the database."`
	Schema schema.Command `cmd:"" help:"Print the database schema."`
}

func main() {
	c := &cli{}
	ctx := kong.Parse(c,
		kong.Name("mnp"),
		kong.Description("Monday Night Pinball data tools."),
		kong.UsageOnError(),
		kong.Bind(newLogger(c.Verbose)),
	)
	ctx.FatalIfErrorf(ctx.Run())
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
