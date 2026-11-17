// Package serve implements the serve command.
package serve

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/negz/mnp/internal/cache"
	"github.com/negz/mnp/internal/live"
	"github.com/negz/mnp/internal/web"
)

// Command starts the MNP web server.
type Command struct {
	Addr      string        `default:":8080"                                                                             help:"Address to listen on."`
	LiveURL   string        `default:"https://www.mondaynightpinball.com"                                                help:"Site to get matches in progress from. Empty disables live matches."`
	LiveTTL   time.Duration `default:"30s"                                                                               help:"How long to remember a match in progress before getting it again."`
	LiveFeeds string        `help:"Read matches in progress from JSON files in this directory instead, for development." hidden:""                                                                 type:"existingdir"`
}

// Run executes the serve command.
func (c *Command) Run(d *cache.DB, _ *slog.Logger) error {
	ctx := context.Background()

	dbst, err := d.Store(ctx)
	if err != nil {
		return err
	}

	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))

	// Serve what's already in the database while the first sync runs.
	st := cache.NewInMemoryStore(dbst)
	if err := st.Refresh(ctx); err != nil {
		log.Error("initial cache refresh failed", "err", err)
	}

	go web.Sync(ctx, func(ctx context.Context) error {
		if err := d.Sync(ctx); err != nil {
			return err
		}
		return st.Refresh(ctx)
	}, 15*time.Minute, log)

	var opts []web.ServerOption
	switch {
	case c.LiveFeeds != "":
		opts = append(opts, web.WithLiveSource(live.DirSource{Dir: c.LiveFeeds}))
	case c.LiveURL != "":
		src := live.NewHTTPSource(c.LiveURL, &http.Client{Timeout: 10 * time.Second},
			live.WithRecordDir(filepath.Join(cache.Dir(), "live")),
			live.WithLogger(log))
		opts = append(opts, web.WithLiveSource(live.NewCachedSource(src, c.LiveTTL)))
	}

	log.Info("Starting web server", "addr", c.Addr)

	s := &http.Server{
		Addr:              c.Addr,
		Handler:           web.WithLogging(web.WithCacheControl(web.NewServer(st, log, opts...).Handler(), "public, max-age=60"), log),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
	}

	return s.ListenAndServe()
}
