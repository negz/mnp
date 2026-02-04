// Package main implements the mnp CLI for syncing Monday Night Pinball data.
package main

import (
	"github.com/alecthomas/kong"

	"github.com/negz/mnp/cmd/mnp/query"
	"github.com/negz/mnp/cmd/mnp/schema"
	"github.com/negz/mnp/cmd/mnp/sync"
)

type cli struct {
	Sync   sync.Command   `cmd:"" help:"Sync data from MNP archive, IPDB, and MatchPlay."`
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
	ctx.FatalIfErrorf(ctx.Run())
}
