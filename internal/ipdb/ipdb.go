// Package ipdb syncs and loads machine metadata from the IPDB database export.
package ipdb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/negz/mnp/internal/db"
)

// DefaultTTL is how long IPDB data is considered fresh before re-syncing.
const DefaultTTL = 7 * 24 * time.Hour

const metadataKey = "ipdb_last_sync"

// MachineStore is the interface for storing machine data.
type MachineStore interface {
	UpsertMachine(ctx context.Context, m db.Machine) error
}

// MetadataStore is the interface for storing sync metadata.
type MetadataStore interface {
	GetMetadata(ctx context.Context, key string) (string, error)
	SetMetadata(ctx context.Context, key, value string) error
}

// Store combines the interfaces needed by the IPDB client.
type Store interface {
	MachineStore
	MetadataStore
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithURL sets the IPDB database JSON URL.
func WithURL(url string) ClientOption {
	return func(c *Client) {
		c.url = url
	}
}

// WithVerbose enables progress output.
func WithVerbose(v bool) ClientOption {
	return func(c *Client) {
		c.verbose = v
	}
}

// WithStore sets the store for loading machine data.
func WithStore(s Store) ClientOption {
	return func(c *Client) {
		c.store = s
	}
}

// WithTTL sets how long cached data is considered fresh.
func WithTTL(d time.Duration) ClientOption {
	return func(c *Client) {
		c.ttl = d
	}
}

// Client syncs and loads IPDB machine metadata.
type Client struct {
	cachePath string
	url       string
	verbose   bool
	store     Store
	ttl       time.Duration
}

// NewClient creates a new IPDB client.
func NewClient(cachePath string, opts ...ClientOption) *Client {
	c := &Client{
		cachePath: cachePath,
		ttl:       DefaultTTL,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Machine represents a machine entry in the IPDB database.
type Machine struct {
	IpdbID            int    `json:"IpdbId"`
	Title             string `json:"Title"`
	Manufacturer      string `json:"Manufacturer"`
	DateOfManufacture string `json:"DateOfManufacture"`
	Type              string `json:"Type"`
	TypeShortName     string `json:"TypeShortName"`
}

type ipdbDatabase struct {
	Data []Machine `json:"Data"`
}

// SyncIfStale syncs IPDB data if the cache is stale or force is true.
// It fetches from the web, loads into the store, and updates the sync timestamp.
func (c *Client) SyncIfStale(ctx context.Context, force bool) error {
	if c.store == nil {
		return fmt.Errorf("no store configured")
	}

	stale, err := c.isStale(ctx, force)
	if err != nil {
		return err
	}
	if !stale {
		return nil
	}

	if c.verbose {
		fmt.Println("Syncing IPDB machine metadata...")
	}

	if err := c.fetch(ctx); err != nil {
		return fmt.Errorf("fetch IPDB: %w", err)
	}
	if err := c.load(ctx); err != nil {
		return fmt.Errorf("load IPDB: %w", err)
	}
	if err := c.store.SetMetadata(ctx, metadataKey, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("update IPDB sync time: %w", err)
	}
	return nil
}

// isStale returns true if the IPDB cache needs refreshing.
func (c *Client) isStale(ctx context.Context, force bool) (bool, error) {
	if force {
		return true, nil
	}

	lastSync, err := c.store.GetMetadata(ctx, metadataKey)
	if err != nil {
		return false, fmt.Errorf("check IPDB sync time: %w", err)
	}
	if lastSync == "" {
		return true, nil
	}

	t, err := time.Parse(time.RFC3339, lastSync)
	if err != nil {
		return true, nil //nolint:nilerr // Unparseable timestamp treated as stale.
	}
	return time.Since(t) > c.ttl, nil
}

// fetch downloads the IPDB database JSON file.
func (c *Client) fetch(ctx context.Context) error {
	if err := os.MkdirAll(c.cachePath, 0o750); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch IPDB: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Nothing useful to do with error.

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch IPDB: %s", resp.Status)
	}

	out, err := os.Create(filepath.Join(c.cachePath, "ipdbdatabase.json"))
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close() //nolint:errcheck // Write already succeeded via io.Copy.

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// load reads the IPDB database and loads machines into the store.
func (c *Client) load(ctx context.Context) error {
	jsonPath := filepath.Join(c.cachePath, "ipdbdatabase.json")

	f, err := os.Open(jsonPath) //nolint:gosec // Path from config.
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.

	var database ipdbDatabase
	if err := json.NewDecoder(f).Decode(&database); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}

	if c.verbose {
		fmt.Printf("  Loading %d machines from IPDB...\n", len(database.Data))
	}

	for _, m := range database.Data {
		year := parseYear(m.DateOfManufacture)
		key := titleToKey(m.Title)
		if key == "" {
			continue
		}

		if err := c.store.UpsertMachine(ctx, db.Machine{
			Key:          key,
			Name:         m.Title,
			Manufacturer: m.Manufacturer,
			Year:         year,
			Type:         m.TypeShortName,
			IPDBID:       m.IpdbID,
		}); err != nil {
			return err
		}
	}

	return nil
}

// parseYear extracts the year from a date string like "1997-06" or "June, 1997".
func parseYear(date string) int {
	formats := []string{
		"2006-01",       // YYYY-MM
		"2006",          // YYYY
		"January, 2006", // Month, YYYY
		"January 2006",  // Month YYYY
		"Jan 2006",      // Mon YYYY
	}
	for _, format := range formats {
		if t, err := time.Parse(format, date); err == nil {
			return t.Year()
		}
	}
	return 0
}

// titleToKey converts a machine title to a key that might match MNP's keys.
// This is imperfect - MNP uses custom abbreviations. We'll need a mapping table.
func titleToKey(title string) string {
	// Remove special characters and spaces for a basic key.
	// This won't match MNP's keys perfectly, but we'll cross-reference later.
	key := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r
		}
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, title)
	return key
}
