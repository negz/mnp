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
	"regexp"
	"strconv"
	"strings"

	"github.com/negz/mnp/internal/db"
)

// Sync downloads the IPDB database JSON file from the given URL.
func Sync(ctx context.Context, url, path string) error {
	if err := os.MkdirAll(path, 0o750); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

	out, err := os.Create(filepath.Join(path, "ipdbdatabase.json")) //nolint:gosec // Path from CLI flag.
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer out.Close() //nolint:errcheck // Write already succeeded via io.Copy.

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
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

// Load reads the IPDB database and loads machines into the store.
func Load(ctx context.Context, store *db.Store, repoPath string) error {
	jsonPath := filepath.Join(repoPath, "ipdbdatabase.json")

	f, err := os.Open(jsonPath) //nolint:gosec // Path from CLI flag.
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.

	var database ipdbDatabase
	if err := json.NewDecoder(f).Decode(&database); err != nil {
		return fmt.Errorf("decode JSON: %w", err)
	}

	fmt.Printf("  Loading %d machines from IPDB...\n", len(database.Data))

	for _, m := range database.Data {
		year := parseYear(m.DateOfManufacture)
		key := titleToKey(m.Title)
		if key == "" {
			continue
		}

		if err := store.UpsertMachine(ctx, db.Machine{
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
	// Try YYYY-MM format first
	if len(date) >= 4 {
		if year, err := strconv.Atoi(date[:4]); err == nil && year > 1900 && year < 2100 {
			return year
		}
	}

	// Try to find a 4-digit year anywhere in the string
	re := regexp.MustCompile(`\b(19|20)\d{2}\b`)
	if match := re.FindString(date); match != "" {
		if year, err := strconv.Atoi(match); err == nil {
			return year
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
