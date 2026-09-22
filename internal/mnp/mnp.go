// Package mnp syncs and loads data from the MNP data archive.
package mnp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"

	"github.com/negz/mnp/internal/db"
)

const (
	// RolePlayer is the default roster role.
	RolePlayer = "P"

	// defaultFullSyncInterval is how long between full rebuilds of the
	// database. Incremental syncs keep the current season fresh; a full
	// rebuild additionally drops teams, venues and players that have left the
	// archive's snapshots.
	defaultFullSyncInterval = 24 * time.Hour

	// lastFullSyncKey is the sync_metadata key holding the RFC3339 time of the
	// last successful full rebuild.
	lastFullSyncKey = "last_full_sync"
)

// A ClientStore loads MNP data and can rebuild itself from scratch. It extends
// the ETL Store with the transaction and metadata operations the sync loop
// needs.
type ClientStore interface {
	Store
	Rebuild(ctx context.Context, fn func(tx *db.SQLiteStore) error) error
	GetMetadata(ctx context.Context, key string) (string, error)
	SetMetadata(ctx context.Context, key, value string) error
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithRepoURL sets the git repository URL.
func WithRepoURL(url string) ClientOption {
	return func(c *Client) {
		c.repoURL = url
	}
}

// WithLogger sets the logger for progress output.
func WithLogger(l *slog.Logger) ClientOption {
	return func(c *Client) {
		c.log = l
	}
}

// WithStore sets the store for loading MNP data.
func WithStore(s ClientStore) ClientOption {
	return func(c *Client) {
		c.store = s
	}
}

// WithFullSyncInterval sets how long between full rebuilds of the database. A
// zero or negative interval makes every sync a full rebuild.
func WithFullSyncInterval(d time.Duration) ClientOption {
	return func(c *Client) {
		c.fullSyncInterval = d
	}
}

// Client syncs and loads MNP archive data.
type Client struct {
	archivePath      string
	repoURL          string
	log              *slog.Logger
	store            ClientStore
	fullSyncInterval time.Duration
}

// NewClient creates a new MNP archive client.
func NewClient(archivePath string, opts ...ClientOption) *Client {
	c := &Client{archivePath: archivePath, fullSyncInterval: defaultFullSyncInterval}
	for _, o := range opts {
		o(c)
	}
	return c
}

// SyncIfStale syncs the git repo and loads data that needs updating. It runs a
// full rebuild when forced or when the last one is older than the full sync
// interval, dropping teams, venues and players no longer in the archive.
// Otherwise it loads incrementally: any season not yet loaded, plus the current
// (max) season, which upserts fresh results without removing departed rows.
func (c *Client) SyncIfStale(ctx context.Context, force bool) error {
	if c.store == nil {
		return fmt.Errorf("no store configured")
	}

	if err := c.pull(ctx); err != nil {
		return fmt.Errorf("sync MNP archive: %w", err)
	}

	available, err := findSeasons(c.archivePath)
	if err != nil {
		return fmt.Errorf("find seasons: %w", err)
	}
	if len(available) == 0 {
		return nil
	}

	full, err := c.needsFullSync(ctx, force)
	if err != nil {
		return err
	}
	if full {
		return c.fullSync(ctx, available)
	}

	return c.incrementalSync(ctx, available)
}

// needsFullSync reports whether the next sync should be a full rebuild. It is
// when forced, when no full sync has run, or when the last one is older than
// the full sync interval.
func (c *Client) needsFullSync(ctx context.Context, force bool) (bool, error) {
	if force || c.fullSyncInterval <= 0 {
		return true, nil
	}
	last, err := c.store.GetMetadata(ctx, lastFullSyncKey)
	if err != nil {
		return false, fmt.Errorf("check last full sync: %w", err)
	}
	if t, terr := time.Parse(time.RFC3339, last); terr == nil {
		return time.Since(t) >= c.fullSyncInterval, nil
	}
	// A missing or corrupt timestamp shouldn't wedge syncing; rebuild, which
	// rewrites it.
	return true, nil
}

// fullSync rebuilds the database from scratch, loading every season, so that
// data removed from the archive's snapshots disappears. It runs in a single
// transaction, so readers keep seeing the previous data until it completes.
func (c *Client) fullSync(ctx context.Context, seasons []int) error {
	c.log.Info("Running full sync", "seasons", len(seasons))
	return c.store.Rebuild(ctx, func(tx *db.SQLiteStore) error {
		if err := c.extractAndLoad(ctx, tx, seasons); err != nil {
			return err
		}
		if err := tx.SetMetadata(ctx, lastFullSyncKey, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return fmt.Errorf("record full sync: %w", err)
		}
		return nil
	})
}

// incrementalSync loads any unloaded season plus the current one, upserting
// into the existing database.
func (c *Client) incrementalSync(ctx context.Context, available []int) error {
	loaded, err := c.store.LoadedSeasons(ctx)
	if err != nil {
		return fmt.Errorf("check loaded seasons: %w", err)
	}

	maxSeason := available[len(available)-1]
	var seasons []int
	for _, s := range available {
		if !loaded[s] || s == maxSeason {
			seasons = append(seasons, s)
		}
	}
	if len(seasons) == 0 {
		return nil
	}

	return c.extractAndLoad(ctx, c.store, seasons)
}

// extractAndLoad reads JSON files from the archive and loads them into the
// store using the ETL types.
func (c *Client) extractAndLoad(ctx context.Context, store Store, seasons []int) error {
	// Machines.
	var machines Machines
	if err := machines.Extract(filepath.Join(c.archivePath, "machines.json")); err != nil {
		return fmt.Errorf("extract machines: %w", err)
	}
	if err := machines.Load(ctx, store); err != nil {
		return fmt.Errorf("load machines: %w", err)
	}

	// Venues.
	var venues Venues
	if err := venues.Extract(filepath.Join(c.archivePath, "venues.json")); err != nil {
		return fmt.Errorf("extract venues: %w", err)
	}
	if err := venues.Load(ctx, store); err != nil {
		return fmt.Errorf("load venues: %w", err)
	}

	// IPRs.
	var iprs IPRs
	if err := iprs.Extract(filepath.Join(c.archivePath, "IPR.csv")); err != nil {
		return fmt.Errorf("extract IPRs: %w", err)
	}
	if err := iprs.Load(ctx, store); err != nil {
		return fmt.Errorf("load IPRs: %w", err)
	}

	// Seasons and matches.
	for _, seasonNum := range seasons {
		c.log.Info("Loading season", "season", seasonNum)
		seasonPath := filepath.Join(c.archivePath, fmt.Sprintf("season-%d", seasonNum))

		var season Season
		if err := season.Extract(filepath.Join(seasonPath, "season.json")); err != nil {
			return fmt.Errorf("extract season %d: %w", seasonNum, err)
		}
		seasonID, err := season.Load(ctx, store, seasonNum)
		if err != nil {
			return fmt.Errorf("load season %d: %w", seasonNum, err)
		}

		var schedule Schedule
		if err := schedule.Extract(filepath.Join(seasonPath, "season.json")); err != nil {
			return fmt.Errorf("extract schedule %d: %w", seasonNum, err)
		}
		if err := schedule.Load(ctx, store, seasonID); err != nil {
			return fmt.Errorf("load schedule %d: %w", seasonNum, err)
		}

		matchFiles, err := findMatchFiles(seasonPath)
		if err != nil {
			return fmt.Errorf("find matches for season %d: %w", seasonNum, err)
		}
		for _, path := range matchFiles {
			var match Match
			if err := match.Extract(path); err != nil {
				c.log.Warn("Failed to extract match", "file", filepath.Base(path), "error", err)
				continue
			}
			if err := match.Load(ctx, store, seasonID); err != nil {
				c.log.Warn("Failed to load match", "file", filepath.Base(path), "error", err)
			}
		}
	}

	return nil
}

// findMatchFiles returns paths to all match JSON files in a season directory.
func findMatchFiles(seasonPath string) ([]string, error) {
	matchesDir := filepath.Join(seasonPath, "matches")
	entries, err := os.ReadDir(matchesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		paths = append(paths, filepath.Join(matchesDir, e.Name()))
	}
	return paths, nil
}

// findSeasons returns available season numbers from the archive.
func findSeasons(archivePath string) ([]int, error) {
	entries, err := os.ReadDir(archivePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
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

// pull clones or updates the MNP data archive.
func (c *Client) pull(ctx context.Context) error {
	if err := os.MkdirAll(filepath.Dir(c.archivePath), 0o750); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	var progress io.Writer
	if c.log.Enabled(ctx, slog.LevelInfo) {
		progress = os.Stderr
	}

	if _, err := os.Stat(filepath.Join(c.archivePath, ".git")); err == nil {
		c.log.Info("Updating MNP archive")
		uerr := c.update(ctx, progress)
		if uerr == nil {
			return nil
		}
		c.log.Info("Update failed, re-cloning", "err", uerr)
		if err := os.RemoveAll(c.archivePath); err != nil {
			return fmt.Errorf("remove corrupt repo: %w", err)
		}
	}

	c.log.Info("Cloning MNP archive")
	_, err := git.PlainCloneContext(ctx, c.archivePath, false, &git.CloneOptions{
		URL:          c.repoURL,
		Depth:        1,
		SingleBranch: true,
		Progress:     progress,
	})
	return err
}

func (c *Client) update(ctx context.Context, progress io.Writer) error {
	r, err := git.PlainOpen(c.archivePath)
	if err != nil {
		return fmt.Errorf("open repo: %w", err)
	}
	w, err := r.Worktree()
	if err != nil {
		return fmt.Errorf("get worktree: %w", err)
	}
	if err := w.Reset(&git.ResetOptions{Mode: git.HardReset}); err != nil {
		return fmt.Errorf("reset worktree: %w", err)
	}
	if err := w.PullContext(ctx, &git.PullOptions{Progress: progress}); err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return err
	}
	return nil
}
