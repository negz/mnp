// Package cache manages the local MNP data cache.
package cache

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/ipdb"
	"github.com/negz/mnp/internal/mnp"
)

// Dir returns the MNP cache directory, using XDG_CACHE_HOME if set.
func Dir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "mnp")
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "mnp")
}

// EnsureDB opens or creates the cache database, initializing the schema.
func EnsureDB(ctx context.Context) (*db.SQLiteStore, error) {
	cacheDir := Dir()

	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return nil, fmt.Errorf("create cache directory: %w", err)
	}

	dbPath := filepath.Join(cacheDir, "mnp.db")
	store, err := db.Open(ctx, dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := store.Init(ctx); err != nil {
		store.Close() //nolint:errcheck // Already returning error.
		return nil, fmt.Errorf("initialize database: %w", err)
	}

	return store, nil
}

// config holds sync configuration.
type config struct {
	ipdbURL    string
	archiveURL string
	force      bool
	log        *slog.Logger
}

// Option configures sync behavior.
type Option func(*config)

// WithIPDBURL sets the IPDB database JSON URL.
func WithIPDBURL(url string) Option {
	return func(c *config) {
		c.ipdbURL = url
	}
}

// WithArchiveURL sets the MNP archive git repo URL.
func WithArchiveURL(url string) Option {
	return func(c *config) {
		c.archiveURL = url
	}
}

// WithForce forces a full re-sync of all data.
func WithForce(f bool) Option {
	return func(c *config) {
		c.force = f
	}
}

// WithLogger sets the logger for sync progress output.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		c.log = l
	}
}

// Sync synchronizes data from upstream sources (IPDB, MNP archive).
// It respects staleness unless force is set.
func Sync(ctx context.Context, store *db.SQLiteStore, opts ...Option) error {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}

	cacheDir := Dir()
	archivePath := filepath.Join(cacheDir, "mnp-data-archive")

	// Create clients with stores injected
	ipdbClient := ipdb.NewClient(filepath.Join(cacheDir, "ipdb"),
		ipdb.WithURL(cfg.ipdbURL),
		ipdb.WithLogger(cfg.log),
		ipdb.WithStore(store),
	)

	mnpClient := mnp.NewClient(archivePath,
		mnp.WithRepoURL(cfg.archiveURL),
		mnp.WithLogger(cfg.log),
		mnp.WithStore(store),
	)

	// Each client handles its own staleness checking and loading
	if err := ipdbClient.SyncIfStale(ctx, cfg.force); err != nil {
		return err
	}

	if err := mnpClient.SyncIfStale(ctx, cfg.force); err != nil {
		return err
	}

	return nil
}
