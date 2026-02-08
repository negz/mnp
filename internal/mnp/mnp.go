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

	"github.com/go-git/go-git/v5"
)

const (
	// RolePlayer is the default roster role.
	RolePlayer = "P"
)

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
func WithStore(s Store) ClientOption {
	return func(c *Client) {
		c.store = s
	}
}

// Client syncs and loads MNP archive data.
type Client struct {
	archivePath string
	repoURL     string
	log         *slog.Logger
	store       Store
}

// NewClient creates a new MNP archive client.
func NewClient(archivePath string, opts ...ClientOption) *Client {
	c := &Client{archivePath: archivePath}
	for _, o := range opts {
		o(c)
	}
	return c
}

// SyncIfStale syncs the git repo and loads any seasons that need updating.
// A season needs loading if: forced, not yet loaded, or is the current (max) season.
func (c *Client) SyncIfStale(ctx context.Context, force bool) error {
	if c.store == nil {
		return fmt.Errorf("no store configured")
	}

	if err := c.pull(ctx); err != nil {
		return fmt.Errorf("sync MNP archive: %w", err)
	}

	loaded, err := c.store.LoadedSeasons(ctx)
	if err != nil {
		return fmt.Errorf("check loaded seasons: %w", err)
	}

	available, err := findSeasons(c.archivePath)
	if err != nil {
		return fmt.Errorf("find seasons: %w", err)
	}
	if len(available) == 0 {
		return nil
	}

	maxSeason := available[len(available)-1]
	var seasons []int
	for _, s := range available {
		if force || !loaded[s] || s == maxSeason {
			seasons = append(seasons, s)
		}
	}
	if len(seasons) == 0 {
		return nil
	}

	if err := c.extractAndLoad(ctx, seasons); err != nil {
		return err
	}

	return nil
}

// extractAndLoad reads JSON files from the archive and loads them into the
// store using the ETL types.
func (c *Client) extractAndLoad(ctx context.Context, seasons []int) error {
	// Machines.
	var machines Machines
	if err := machines.Extract(filepath.Join(c.archivePath, "machines.json")); err != nil {
		return fmt.Errorf("extract machines: %w", err)
	}
	if err := machines.Load(ctx, c.store); err != nil {
		return fmt.Errorf("load machines: %w", err)
	}

	// Venues.
	var venues Venues
	if err := venues.Extract(filepath.Join(c.archivePath, "venues.json")); err != nil {
		return fmt.Errorf("extract venues: %w", err)
	}
	if err := venues.Load(ctx, c.store); err != nil {
		return fmt.Errorf("load venues: %w", err)
	}

	// Seasons and matches.
	for _, seasonNum := range seasons {
		c.log.Info("Loading season", "season", seasonNum)
		seasonPath := filepath.Join(c.archivePath, fmt.Sprintf("season-%d", seasonNum))

		var season Season
		if err := season.Extract(filepath.Join(seasonPath, "season.json")); err != nil {
			return fmt.Errorf("extract season %d: %w", seasonNum, err)
		}
		seasonID, err := season.Load(ctx, c.store, seasonNum)
		if err != nil {
			return fmt.Errorf("load season %d: %w", seasonNum, err)
		}

		var schedule Schedule
		if err := schedule.Extract(filepath.Join(seasonPath, "season.json")); err != nil {
			return fmt.Errorf("extract schedule %d: %w", seasonNum, err)
		}
		if err := schedule.Load(ctx, c.store, seasonID); err != nil {
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
			if err := match.Load(ctx, c.store, seasonID); err != nil {
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

	c.log.Info("Cloning MNP archive")
	_, err := git.PlainCloneContext(ctx, c.archivePath, false, &git.CloneOptions{
		URL:          c.repoURL,
		Depth:        1,
		SingleBranch: true,
		Progress:     progress,
	})
	return err
}
