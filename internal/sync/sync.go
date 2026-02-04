// Package sync provides on-demand data synchronization.
package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/negz/mnp/internal/db"
	"github.com/negz/mnp/internal/ipdb"
	"github.com/negz/mnp/internal/mnp"
)

const (
	// IPDBTTL is how long IPDB data is considered fresh.
	IPDBTTL = 7 * 24 * time.Hour

	ipdbSyncKey = "ipdb_last_sync"
)

// CacheDir returns the MNP cache directory, using XDG_CACHE_HOME if set.
func CacheDir() string {
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

// Options configures the sync behavior.
type Options struct {
	IPDBURL    string
	ArchiveURL string
	Force      bool
	Verbose    bool
}

// EnsureDB opens or creates the database, syncing data as needed.
func EnsureDB(ctx context.Context, opts Options) (*db.Store, error) {
	cacheDir := CacheDir()

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

	if err := syncIfNeeded(ctx, store, cacheDir, opts); err != nil {
		store.Close() //nolint:errcheck // Already returning error.
		return nil, err
	}

	return store, nil
}

func syncIfNeeded(ctx context.Context, store *db.Store, cacheDir string, opts Options) error {
	archivePath := filepath.Join(cacheDir, "mnp-data-archive")

	// Git pull (fast when up-to-date)
	if err := mnp.Sync(ctx, opts.ArchiveURL, archivePath, opts.Verbose); err != nil {
		return fmt.Errorf("sync MNP archive: %w", err)
	}

	// Determine what needs syncing
	ipdbStale, err := isIPDBStale(ctx, store, opts.Force)
	if err != nil {
		return err
	}

	seasonsToLoad, err := seasonsToLoad(ctx, store, archivePath, opts.Force)
	if err != nil {
		return err
	}

	if !ipdbStale && len(seasonsToLoad) == 0 {
		return nil
	}

	// Sync IPDB if stale
	if ipdbStale {
		if err := syncIPDB(ctx, store, cacheDir, opts); err != nil {
			return err
		}
	}

	// Load MNP data
	if len(seasonsToLoad) > 0 {
		if err := loadMNPData(ctx, store, archivePath, seasonsToLoad, opts.Verbose); err != nil {
			return err
		}
	}

	return nil
}

func isIPDBStale(ctx context.Context, store *db.Store, force bool) (bool, error) {
	if force {
		return true, nil
	}

	lastSync, err := store.GetMetadata(ctx, ipdbSyncKey)
	if err != nil {
		return false, fmt.Errorf("check IPDB sync time: %w", err)
	}
	if lastSync == "" {
		return true, nil
	}

	t, err := time.Parse(time.RFC3339, lastSync)
	if err != nil {
		return true, nil //nolint:nilerr // Unparseable timestamp is treated as stale.
	}
	return time.Since(t) > IPDBTTL, nil
}

func seasonsToLoad(ctx context.Context, store *db.Store, archivePath string, force bool) ([]int, error) {
	loaded, err := store.LoadedSeasons(ctx)
	if err != nil {
		return nil, fmt.Errorf("check loaded seasons: %w", err)
	}

	available, err := findSeasons(archivePath)
	if err != nil {
		return nil, fmt.Errorf("find seasons: %w", err)
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("no seasons found in archive")
	}

	maxSeason := available[len(available)-1]
	var toLoad []int
	for _, s := range available {
		if force || !loaded[s] || s == maxSeason {
			toLoad = append(toLoad, s)
		}
	}
	return toLoad, nil
}

func syncIPDB(ctx context.Context, store *db.Store, cacheDir string, opts Options) error {
	if opts.Verbose {
		fmt.Println("Syncing IPDB machine metadata...")
	}

	ipdbPath := filepath.Join(cacheDir, "ipdb")
	if err := ipdb.Sync(ctx, opts.IPDBURL, ipdbPath); err != nil {
		return fmt.Errorf("sync IPDB: %w", err)
	}
	if err := ipdb.Load(ctx, store, ipdbPath); err != nil {
		return fmt.Errorf("load IPDB: %w", err)
	}
	if err := store.SetMetadata(ctx, ipdbSyncKey, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("update IPDB sync time: %w", err)
	}
	return nil
}

func loadMNPData(ctx context.Context, store *db.Store, archivePath string, seasons []int, verbose bool) error {
	if err := mnp.LoadGlobals(ctx, store, archivePath); err != nil {
		return fmt.Errorf("load global data: %w", err)
	}

	for _, s := range seasons {
		if verbose {
			fmt.Printf("Loading season %d...\n", s)
		}
		seasonPath := filepath.Join(archivePath, fmt.Sprintf("season-%d", s))
		if err := mnp.LoadSeason(ctx, store, seasonPath, s); err != nil {
			return fmt.Errorf("load season %d: %w", s, err)
		}
	}
	return nil
}

func findSeasons(archivePath string) ([]int, error) {
	entries, err := os.ReadDir(archivePath)
	if err != nil {
		return nil, err
	}

	seasons := make([]int, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "season-") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "season-"))
		if err != nil {
			continue
		}
		seasons = append(seasons, n)
	}
	sort.Ints(seasons)
	return seasons, nil
}
