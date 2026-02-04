// Package schema implements the schema command.
package schema

import (
	"fmt"

	"github.com/negz/mnp/internal/db"
)

// Command prints the database schema.
type Command struct{}

// Run executes the schema command.
func (c *Command) Run() error {
	fmt.Print(db.Schema())
	return nil
}
