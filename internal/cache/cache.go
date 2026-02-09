// Package cache manages the local MNP data cache.
package cache

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/mnp"
)

// Dir returns the MNP cache directory.
//
// It uses os.UserCacheDir, which respects XDG_CACHE_HOME on Linux, uses
// ~/Library/Caches on macOS, and %LocalAppData% on Windows. If the user cache
// directory can't be determined it falls back to the system temp directory.
func Dir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "mnp")
	}
	return filepath.Join(base, "mnp")
}

// DB provides access to an MNP database.
// It lazily opens the database on first use.
type DB struct {
	ArchiveURL string `default:"https://github.com/Invader-Zim/mnp-data-archive.git" help:"MNP archive git repo URL." hidden:""`
	ForceSync  bool   `help:"Sync data before running command."                      name:"sync"                      short:"s"`

	log   *slog.Logger
	store *db.SQLiteStore
}

// SetLogger configures the logger for sync progress.
func (d *DB) SetLogger(log *slog.Logger) {
	d.log = log
}

// Store returns the database store, opening it if needed. It does not sync
// data from the archive. Use SyncedStore when the caller needs fresh data
// before proceeding.
func (d *DB) Store(ctx context.Context) (*db.SQLiteStore, error) {
	if d.store != nil {
		return d.store, nil
	}

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

	d.store = store
	return d.store, nil
}

// SyncedStore returns the database store, syncing data from the archive first.
func (d *DB) SyncedStore(ctx context.Context) (*db.SQLiteStore, error) {
	store, err := d.Store(ctx)
	if err != nil {
		return nil, err
	}

	if err := d.Sync(ctx); err != nil {
		d.store.Close() //nolint:errcheck // Already returning error.
		d.store = nil
		return nil, err
	}

	return store, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	if d.store == nil {
		return nil
	}
	return d.store.Close()
}

// Sync synchronizes data from the MNP data archive.
// It respects staleness unless ForceSync is set.
func (d *DB) Sync(ctx context.Context) error {
	archivePath := filepath.Join(Dir(), "mnp-data-archive")

	mnpClient := mnp.NewClient(archivePath,
		mnp.WithRepoURL(d.ArchiveURL),
		mnp.WithLogger(d.log),
		mnp.WithStore(d.store),
	)

	return mnpClient.SyncIfStale(ctx, d.ForceSync)
}
