// Package main implements the mnp CLI for querying Monday Night Pinball data.
package main

import (
	"log/slog"
	"os"

	"github.com/alecthomas/kong"

	"github.com/negz/mnp/cmd/mnp/machines"
	"github.com/negz/mnp/cmd/mnp/matchup"
	"github.com/negz/mnp/cmd/mnp/query"
	"github.com/negz/mnp/cmd/mnp/recommend"
	"github.com/negz/mnp/cmd/mnp/schema"
	"github.com/negz/mnp/cmd/mnp/scout"
	"github.com/negz/mnp/cmd/mnp/teams"
	"github.com/negz/mnp/cmd/mnp/venues"
	"github.com/negz/mnp/internal/cache"
)

type cli struct {
	Verbose bool `help:"Print sync progress." short:"v"`

	Recommend recommend.Command `cmd:"" help:"Recommend players for a machine."`
	Scout     scout.Command     `cmd:"" help:"Scout a team's strengths and weaknesses."`
	Matchup   matchup.Command   `cmd:"" help:"Compare two teams head-to-head at a venue."`
	Teams     teams.Command     `cmd:"" help:"List all teams."`
	Venues    venues.Command    `cmd:"" help:"List all venues."`
	Machines  machines.Command  `cmd:"" help:"List all machines."`
	DB        dbCmd             `cmd:"" help:"Database utilities."`

	Cache cache.DB `embed:""`
}

type dbCmd struct {
	Query  query.Command  `cmd:"" help:"Run a SQL query against the database."`
	Schema schema.Command `cmd:"" help:"Print the database schema."`
}

func main() {
	c := &cli{}
	ctx := kong.Parse(c,
		kong.Name("mnp"),
		kong.Description("Monday Night Pinball data tools."),
		kong.UsageOnError(),
	)

	defer c.Cache.Close() //nolint:errcheck // Not much we can do about this.

	level := slog.LevelWarn
	if c.Verbose {
		level = slog.LevelInfo
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	c.Cache.SetLogger(log)
	ctx.Bind(log, &c.Cache)

	ctx.FatalIfErrorf(ctx.Run())
}
