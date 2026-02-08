// Package db implements the db command group.
package db

import (
	"github.com/negz/mnp/cmd/mnp/db/query"
	"github.com/negz/mnp/cmd/mnp/db/schema"
)

// Command groups database utility subcommands.
type Command struct {
	Query  query.Command  `cmd:"" help:"Run a SQL query against the database."`
	Schema schema.Command `cmd:"" help:"Print the database schema."`
}
