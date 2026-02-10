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

	dbStore, err := d.Store(ctx)
	if err != nil {
		return err
	}

	store := cache.NewInMemoryStore(dbStore)

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	syncAndRefresh := func(ctx context.Context) error {
		if err := d.Sync(ctx); err != nil {
			return err
		}
		return store.Refresh(ctx)
	}
	go web.Sync(ctx, syncAndRefresh, 15*time.Minute, log)

	handler := web.WithLogging(web.WithCacheControl(web.NewServer(store, log).Handler(), "public, max-age=60"), log)

	log.Info("Starting web server", "addr", c.Addr)

	s := &http.Server{
		Addr:              c.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	return s.ListenAndServe()
}
