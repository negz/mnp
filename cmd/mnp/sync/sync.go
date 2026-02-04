// Package sync implements the sync command.
package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/ipdb"
	"github.com/negz/mnp/internal/mnp"
)

// Command syncs MNP data to a local SQLite database.
type Command struct {
	DBPath   string `default:"mnp.db"   help:"Path to SQLite database."`
	CacheDir string `default:"/tmp/mnp" help:"Directory for cloned repos."`

	IPDBURL    string `default:"https://raw.githubusercontent.com/xantari/Ipdb.Database/refs/heads/master/Ipdb.Database/Database/ipdbdatabase.json" help:"IPDB database JSON URL."   name:"ipdb-url"`
	ArchiveURL string `default:"https://github.com/Invader-Zim/mnp-data-archive.git"                                                                help:"MNP archive git repo URL."`
}

// Run executes the sync command.
func (c *Command) Run() error {
	ctx := context.Background()

	if err := os.MkdirAll(c.CacheDir, 0o750); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}

	dbPath := expandPath(c.DBPath)
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create database directory: %w", err)
		}
	}

	store, err := db.Open(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close() //nolint:errcheck // Nothing to do with error on program exit.

	if err := store.Init(ctx); err != nil {
		return fmt.Errorf("initialize database: %w", err)
	}

	fmt.Println("Syncing IPDB machine metadata...")
	ipdbPath := filepath.Join(c.CacheDir, "ipdb")
	if err := ipdb.Sync(ctx, c.IPDBURL, ipdbPath); err != nil {
		return fmt.Errorf("sync IPDB: %w", err)
	}
	if err := ipdb.Load(ctx, store, ipdbPath); err != nil {
		return fmt.Errorf("load IPDB: %w", err)
	}

	fmt.Println("Syncing MNP archive...")
	archivePath := filepath.Join(c.CacheDir, "mnp-data-archive")
	if err := mnp.Sync(ctx, c.ArchiveURL, archivePath); err != nil {
		return fmt.Errorf("sync MNP archive: %w", err)
	}
	if err := mnp.Load(ctx, store, archivePath); err != nil {
		return fmt.Errorf("load MNP archive: %w", err)
	}

	fmt.Println("Sync complete.")
	return nil
}

func expandPath(path string) string {
	if len(path) > 0 && path[0] == '~' {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}
