// Package serve implements the serve command.
package serve

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/web"
)

// Command starts the MNP web server.
type Command struct {
	Addr string `default:":8080" help:"Address to listen on."`
}

// Run executes the serve command.
func (c *Command) Run(d *cache.DB, _ *slog.Logger) error {
	ctx := context.Background()

	store, err := d.Store(ctx)
	if err != nil {
		return err
	}

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	go web.Sync(ctx, d.Sync, 15*time.Minute, log)

	handler := web.WithLogging(web.NewServer(store, log).Handler(), log)

	log.Info("Starting web server", "addr", c.Addr)

	s := &http.Server{
		Addr:              c.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return s.ListenAndServe()
}
