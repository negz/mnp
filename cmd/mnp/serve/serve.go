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

	dbst, err := d.Store(ctx)
	if err != nil {
		return err
	}

	st := cache.NewInMemoryStore(dbst)

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	go web.Sync(ctx, func(ctx context.Context) error {
		if err := d.Sync(ctx); err != nil {
			return err
		}
		return st.Refresh(ctx)
	}, 15*time.Minute, log)

	log.Info("Starting web server", "addr", c.Addr)

	s := &http.Server{
		Addr:              c.Addr,
		Handler:           web.WithLogging(web.WithCacheControl(web.NewServer(st, log).Handler(), "public, max-age=60"), log),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	return s.ListenAndServe()
}
