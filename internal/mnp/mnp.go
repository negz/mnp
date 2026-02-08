// Package mnp syncs and loads data from the MNP data archive.
package mnp

import (
	"context"
	"encoding/json"
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

	"github.com/negz/mnp/internal/db"
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
func WithStore(s *db.SQLiteStore) ClientOption {
	return func(c *Client) {
		c.store = s
	}
}

// Client syncs and loads MNP archive data.
type Client struct {
	archivePath string
	repoURL     string
	log         *slog.Logger
	store       *db.SQLiteStore
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

	available, err := c.findSeasons()
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

	if err := c.loadMachines(ctx); err != nil {
		return fmt.Errorf("load machines: %w", err)
	}
	if err := c.loadVenues(ctx); err != nil {
		return fmt.Errorf("load venues: %w", err)
	}

	for _, s := range seasons {
		c.log.Info("Loading season", "season", s)
		if err := c.loadSeason(ctx, s); err != nil {
			return fmt.Errorf("load season %d: %w", s, err)
		}
	}

	return nil
}

// findSeasons returns available season numbers from the archive.
func (c *Client) findSeasons() ([]int, error) {
	entries, err := os.ReadDir(c.archivePath)
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

func (c *Client) loadMachines(ctx context.Context) error {
	f, err := os.Open(filepath.Join(c.archivePath, "machines.json"))
	if err != nil {
		return fmt.Errorf("open machines.json: %w", err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.

	var machines map[string]struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(f).Decode(&machines); err != nil {
		return fmt.Errorf("decode machines.json: %w", err)
	}

	for _, m := range machines {
		if err := c.store.UpsertMachine(ctx, db.Machine{
			Key:  m.Key,
			Name: m.Name,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) loadVenues(ctx context.Context) error {
	f, err := os.Open(filepath.Join(c.archivePath, "venues.json"))
	if err != nil {
		return fmt.Errorf("open venues.json: %w", err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.

	var venues map[string]struct {
		Key      string   `json:"key"`
		Name     string   `json:"name"`
		Machines []string `json:"machines"`
	}
	if err := json.NewDecoder(f).Decode(&venues); err != nil {
		return fmt.Errorf("decode venues.json: %w", err)
	}

	knownMachines, err := c.store.ListMachineKeys(ctx)
	if err != nil {
		return fmt.Errorf("list machine keys: %w", err)
	}

	for _, v := range venues {
		venueID, err := c.store.UpsertVenue(ctx, v.Key, v.Name)
		if err != nil {
			return err
		}
		for _, mk := range v.Machines {
			if !knownMachines[mk] {
				c.log.Info("Skipping unknown venue machine", "venue", v.Key, "machine", mk)
				continue
			}
			if err := c.store.UpsertVenueMachine(ctx, venueID, mk); err != nil {
				return err
			}
		}
	}

	return nil
}

// loadSeason loads a single season's data (teams, rosters, matches).
func (c *Client) loadSeason(ctx context.Context, seasonNum int) error {
	seasonPath := filepath.Join(c.archivePath, fmt.Sprintf("season-%d", seasonNum))

	seasonID, err := c.store.UpsertSeason(ctx, seasonNum)
	if err != nil {
		return err
	}

	// Load season.json for teams and rosters
	if err := c.loadSeasonJSON(ctx, seasonPath, seasonID); err != nil {
		return fmt.Errorf("load season.json: %w", err)
	}

	// Load individual match files
	matchesDir := filepath.Join(seasonPath, "matches")
	entries, err := os.ReadDir(matchesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No matches yet
		}
		return fmt.Errorf("read matches dir: %w", err)
	}

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		matchPath := filepath.Join(matchesDir, e.Name())
		if err := c.loadMatch(ctx, matchPath, seasonID); err != nil {
			c.log.Warn("Failed to load match", "file", e.Name(), "error", err)
		}
	}

	return nil
}

type seasonJSON struct {
	Teams map[string]struct {
		Key    string `json:"key"`
		Venue  string `json:"venue"`
		Name   string `json:"name"`
		Roster []struct {
			Name string `json:"name"`
		} `json:"roster"`
	} `json:"teams"`
}

func (c *Client) loadSeasonJSON(ctx context.Context, seasonPath string, seasonID int64) error {
	f, err := os.Open(filepath.Join(seasonPath, "season.json")) //nolint:gosec // Internal archive path.
	if err != nil {
		return fmt.Errorf("open season.json: %w", err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.

	var season seasonJSON
	if err := json.NewDecoder(f).Decode(&season); err != nil {
		return fmt.Errorf("decode season.json: %w", err)
	}

	for _, t := range season.Teams {
		var venueID int64
		if t.Venue != "" {
			var err error
			venueID, err = c.store.UpsertVenue(ctx, t.Venue, t.Venue)
			if err != nil {
				return fmt.Errorf("upsert venue %s: %w", t.Venue, err)
			}
		}

		teamID, err := c.store.UpsertTeam(ctx, db.Team{
			Key:         t.Key,
			Name:        t.Name,
			SeasonID:    seasonID,
			HomeVenueID: venueID,
		})
		if err != nil {
			return err
		}

		// Load roster
		for _, p := range t.Roster {
			playerID, err := c.store.UpsertPlayer(ctx, p.Name)
			if err != nil {
				return err
			}
			if err := c.store.UpsertRoster(ctx, playerID, teamID, RolePlayer); err != nil {
				return err
			}
		}
	}

	return nil
}

type matchJSON struct {
	Key    string        `json:"key"`
	Week   string        `json:"week"`
	Date   string        `json:"date"`
	State  string        `json:"state"`
	Venue  venueJSON     `json:"venue"`
	Home   teamMatchJSON `json:"home"`
	Away   teamMatchJSON `json:"away"`
	Rounds []roundJSON   `json:"rounds"`
}

type venueJSON struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type teamMatchJSON struct {
	Key    string       `json:"key"`
	Name   string       `json:"name"`
	Points int          `json:"points"`
	Lineup []lineupJSON `json:"lineup"`
}

type lineupJSON struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type roundJSON struct {
	N     int        `json:"n"`
	Games []gameJSON `json:"games"`
}

type gameJSON struct {
	N       int     `json:"n"`
	Machine string  `json:"machine"`
	Done    bool    `json:"done"`
	Player1 string  `json:"player_1"`
	Player2 string  `json:"player_2"`
	Player3 string  `json:"player_3"`
	Player4 string  `json:"player_4"`
	Score1  int64   `json:"score_1"`
	Score2  int64   `json:"score_2"`
	Score3  int64   `json:"score_3"`
	Score4  int64   `json:"score_4"`
	Points1 float64 `json:"points_1"`
	Points2 float64 `json:"points_2"`
	Points3 float64 `json:"points_3"`
	Points4 float64 `json:"points_4"`
}

// playerResult holds data needed to insert a game result.
type playerResult struct {
	hash   string
	score  int64
	pos    int
	teamID int64
}

// buildPlayerResults constructs the player results for a game.
// In doubles (rounds 1,4): players 1,3 are away team, 2,4 are home team.
// In singles (rounds 2,3): player 1 is picking team, player 2 is matching team.
func buildPlayerResults(g gameJSON, homeTeamID, awayTeamID int64, isDoubles bool) []playerResult {
	results := []playerResult{
		{hash: g.Player1, score: g.Score1, pos: 1, teamID: awayTeamID},
		{hash: g.Player2, score: g.Score2, pos: 2, teamID: homeTeamID},
	}
	if isDoubles {
		results = append(results,
			playerResult{hash: g.Player3, score: g.Score3, pos: 3, teamID: awayTeamID},
			playerResult{hash: g.Player4, score: g.Score4, pos: 4, teamID: homeTeamID},
		)
	}
	return results
}

//nolint:gocognit // Linear orchestration loop: parse JSON, resolve IDs, insert games/results.
func (c *Client) loadMatch(ctx context.Context, matchPath string, seasonID int64) error {
	f, err := os.Open(matchPath) //nolint:gosec // Internal archive path.
	if err != nil {
		return fmt.Errorf("open match file: %w", err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.

	var m matchJSON
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return fmt.Errorf("decode match: %w", err)
	}

	// Build player hash -> name map from lineups
	playerNames := make(map[string]string)
	for _, p := range m.Home.Lineup {
		playerNames[p.Key] = p.Name
	}
	for _, p := range m.Away.Lineup {
		playerNames[p.Key] = p.Name
	}

	// Get venue ID
	var venueID int64
	if m.Venue.Key != "" {
		var err error
		venueID, err = c.store.UpsertVenue(ctx, m.Venue.Key, m.Venue.Name)
		if err != nil {
			return fmt.Errorf("upsert venue %s: %w", m.Venue.Key, err)
		}
	}

	// Get team IDs
	homeTeamID, err := c.store.GetTeamID(ctx, m.Home.Key, seasonID)
	if err != nil {
		return fmt.Errorf("get home team: %w", err)
	}
	awayTeamID, err := c.store.GetTeamID(ctx, m.Away.Key, seasonID)
	if err != nil {
		return fmt.Errorf("get away team: %w", err)
	}

	week, _ := strconv.Atoi(m.Week)
	matchID, err := c.store.UpsertMatch(ctx, db.Match{
		Key:        m.Key,
		SeasonID:   seasonID,
		Week:       week,
		Date:       m.Date,
		HomeTeamID: homeTeamID,
		AwayTeamID: awayTeamID,
		VenueID:    venueID,
	})
	if err != nil {
		return err
	}

	if err := c.store.DeleteMatchGames(ctx, matchID); err != nil {
		return err
	}

	for _, r := range m.Rounds {
		isDoubles := r.N == 1 || r.N == 4

		for _, g := range r.Games {
			if !g.Done {
				continue
			}

			gameID, err := c.store.InsertGame(ctx, db.Game{
				MatchID:    matchID,
				Round:      r.N,
				MachineKey: g.Machine,
				IsDoubles:  isDoubles,
			})
			if err != nil {
				return err
			}

			for _, p := range buildPlayerResults(g, homeTeamID, awayTeamID, isDoubles) {
				if p.hash == "" {
					continue
				}

				name, ok := playerNames[p.hash]
				if !ok {
					continue
				}

				playerID, err := c.store.UpsertPlayer(ctx, name)
				if err != nil {
					return err
				}

				if err := c.store.InsertGameResult(ctx, db.GameResult{
					GameID:   gameID,
					PlayerID: playerID,
					TeamID:   p.teamID,
					Position: p.pos,
					Score:    p.score,
				}); err != nil {
					return err
				}
			}
		}
	}

	return nil
}
