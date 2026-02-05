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

// DB provides access to a synced MNP database.
// It lazily opens and syncs the database on first use.
type DB struct {
	IPDBURL    string `default:"https://raw.githubusercontent.com/xantari/Ipdb.Database/refs/heads/master/Ipdb.Database/Database/ipdbdatabase.json" help:"IPDB database JSON URL."   hidden:"" name:"ipdb-url"`
	ArchiveURL string `default:"https://github.com/Invader-Zim/mnp-data-archive.git"                                                                help:"MNP archive git repo URL." hidden:""`
	ForceSync  bool   `help:"Sync data before running command."                                                                                     name:"sync"                      short:"s"`

	log   *slog.Logger
	store *db.SQLiteStore
}

// SetLogger configures the logger for sync progress.
func (d *DB) SetLogger(log *slog.Logger) {
	d.log = log
}

// Store returns the database store, opening and syncing if needed.
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

	if err := d.Sync(ctx); err != nil {
		d.store.Close() //nolint:errcheck // Already returning error.
		d.store = nil
		return nil, err
	}

	return d.store, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	if d.store == nil {
		return nil
	}
	return d.store.Close()
}

// Sync synchronizes data from upstream sources (IPDB, MNP archive).
// It respects staleness unless ForceSync is set.
func (d *DB) Sync(ctx context.Context) error {
	cacheDir := Dir()
	archivePath := filepath.Join(cacheDir, "mnp-data-archive")

	ipdbClient := ipdb.NewClient(filepath.Join(cacheDir, "ipdb"),
		ipdb.WithURL(d.IPDBURL),
		ipdb.WithLogger(d.log),
		ipdb.WithStore(d.store),
	)

	mnpClient := mnp.NewClient(archivePath,
		mnp.WithRepoURL(d.ArchiveURL),
		mnp.WithLogger(d.log),
		mnp.WithStore(d.store),
	)

	if err := ipdbClient.SyncIfStale(ctx, d.ForceSync); err != nil {
		return err
	}

	if err := mnpClient.SyncIfStale(ctx, d.ForceSync); err != nil {
		return err
	}

	return nil
}
