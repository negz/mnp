// Package mnp syncs and loads data from the MNP data archive.
package mnp

import (
	"context"
	"database/sql"
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

// Store is the interface for MNP data storage operations.
//
//nolint:interfacebloat // Cohesive domain interface; all methods needed for MNP data loading.
type Store interface {
	// UpsertMachine inserts or updates a machine.
	UpsertMachine(ctx context.Context, m db.Machine) error

	// UpsertVenue inserts or updates a venue and returns its ID.
	UpsertVenue(ctx context.Context, key, name string) (int64, error)

	// UpsertSeason inserts or updates a season and returns its ID.
	UpsertSeason(ctx context.Context, number int) (int64, error)

	// UpsertTeam inserts or updates a team and returns its ID.
	UpsertTeam(ctx context.Context, t db.Team) (int64, error)

	// UpsertPlayer inserts or updates a player and returns their ID.
	UpsertPlayer(ctx context.Context, name string) (int64, error)

	// UpsertRoster adds a player to a team roster.
	UpsertRoster(ctx context.Context, playerID, teamID int64, role string) error

	// UpsertVenueMachine associates a machine with a venue.
	UpsertVenueMachine(ctx context.Context, venueID int64, machineKey string) error

	// UpsertMatch inserts or updates a match and returns its ID.
	UpsertMatch(ctx context.Context, m db.Match) (int64, error)

	// DeleteMatchGames deletes all games and results for a match (for re-import).
	DeleteMatchGames(ctx context.Context, matchID int64) error

	// InsertGame inserts a game and returns its ID.
	InsertGame(ctx context.Context, g db.Game) (int64, error)

	// InsertGameResult inserts a game result.
	InsertGameResult(ctx context.Context, r db.GameResult) error

	// LoadedSeasons returns season numbers that have at least one match loaded.
	LoadedSeasons(ctx context.Context) (map[int]bool, error)

	// DB returns the underlying database for direct queries.
	DB() *sql.DB
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

	// Git pull (fast when up-to-date)
	if err := c.pull(ctx); err != nil {
		return fmt.Errorf("sync MNP archive: %w", err)
	}

	// Determine which seasons need loading
	seasons, err := c.seasonsToLoad(ctx, force)
	if err != nil {
		return err
	}

	if len(seasons) == 0 {
		return nil
	}

	// Load global data first
	if err := c.loadGlobals(ctx); err != nil {
		return fmt.Errorf("load global data: %w", err)
	}

	// Load each season
	for _, s := range seasons {
		c.log.Info("Loading season", "season", s)
		if err := c.loadSeason(ctx, s); err != nil {
			return fmt.Errorf("load season %d: %w", s, err)
		}
	}

	return nil
}

// seasonsToLoad returns which seasons need to be loaded into the store.
func (c *Client) seasonsToLoad(ctx context.Context, force bool) ([]int, error) {
	loaded, err := c.store.LoadedSeasons(ctx)
	if err != nil {
		return nil, fmt.Errorf("check loaded seasons: %w", err)
	}

	available, err := c.findSeasons()
	if err != nil {
		return nil, fmt.Errorf("find seasons: %w", err)
	}
	if len(available) == 0 {
		return nil, nil // No seasons in archive yet
	}

	maxSeason := available[len(available)-1]
	toLoad := make([]int, 0, len(available))
	for _, s := range available {
		if force || !loaded[s] || s == maxSeason {
			toLoad = append(toLoad, s)
		}
	}
	return toLoad, nil
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

// loadGlobals loads machines.json and venues.json from the archive root.
func (c *Client) loadGlobals(ctx context.Context) error {
	if err := c.loadMachines(ctx); err != nil {
		return fmt.Errorf("load machines: %w", err)
	}
	if err := c.loadVenues(ctx); err != nil {
		return fmt.Errorf("load venues: %w", err)
	}
	return nil
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

	// Build set of known machines to skip venue machines that don't exist.
	knownMachines := make(map[string]bool)
	mrows, err := c.store.DB().QueryContext(ctx, "SELECT key FROM machines")
	if err != nil {
		return fmt.Errorf("query machines: %w", err)
	}
	defer mrows.Close() //nolint:errcheck // Read-only query.
	for mrows.Next() {
		var k string
		if err := mrows.Scan(&k); err != nil {
			return fmt.Errorf("scan machine key: %w", err)
		}
		knownMachines[k] = true
	}
	if err := mrows.Err(); err != nil {
		return fmt.Errorf("iterate machines: %w", err)
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
		// Get venue ID
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
	points float64
	pos    int
	teamID int64
}

// buildPlayerResults constructs the player results for a game.
// In doubles (rounds 1,4): players 1,3 are away team, 2,4 are home team.
// In singles (rounds 2,3): player 1 is picking team, player 2 is matching team.
func buildPlayerResults(g gameJSON, homeTeamID, awayTeamID int64, isDoubles bool) []playerResult {
	results := []playerResult{
		{g.Player1, g.Score1, g.Points1, 1, awayTeamID},
		{g.Player2, g.Score2, g.Points2, 2, homeTeamID},
	}
	if isDoubles {
		results = append(results,
			playerResult{g.Player3, g.Score3, g.Points3, 3, awayTeamID},
			playerResult{g.Player4, g.Score4, g.Points4, 4, homeTeamID},
		)
	}
	return results
}

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
	homeTeamID, err := c.getTeamID(ctx, m.Home.Key, seasonID)
	if err != nil {
		return fmt.Errorf("get home team: %w", err)
	}
	awayTeamID, err := c.getTeamID(ctx, m.Away.Key, seasonID)
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
		HomePoints: m.Home.Points,
		AwayPoints: m.Away.Points,
	})
	if err != nil {
		return err
	}

	// Delete existing games for this match (for re-import)
	if err := c.store.DeleteMatchGames(ctx, matchID); err != nil {
		return err
	}

	return c.loadMatchGames(ctx, matchID, m.Rounds, homeTeamID, awayTeamID, playerNames)
}

func (c *Client) loadMatchGames(ctx context.Context, matchID int64, rounds []roundJSON, homeTeamID, awayTeamID int64, playerNames map[string]string) error {
	for _, r := range rounds {
		isDoubles := r.N == 1 || r.N == 4
		pointsPossible := 3 // singles
		if isDoubles {
			pointsPossible = 5
		}

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

			if err := c.insertGameResults(ctx, gameID, g, homeTeamID, awayTeamID, playerNames, pointsPossible, isDoubles); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) insertGameResults(ctx context.Context, gameID int64, g gameJSON, homeTeamID, awayTeamID int64, playerNames map[string]string, pointsPossible int, isDoubles bool) error {
	for _, p := range buildPlayerResults(g, homeTeamID, awayTeamID, isDoubles) {
		if p.hash == "" {
			continue
		}

		name, ok := playerNames[p.hash]
		if !ok {
			continue // Unknown player
		}

		playerID, err := c.store.UpsertPlayer(ctx, name)
		if err != nil {
			return err
		}

		if err := c.store.InsertGameResult(ctx, db.GameResult{
			GameID:         gameID,
			PlayerID:       playerID,
			TeamID:         p.teamID,
			Position:       p.pos,
			Score:          p.score,
			PointsWon:      int(p.points * 2), // Store as half-points to avoid float
			PointsPossible: pointsPossible * 2,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) getTeamID(ctx context.Context, key string, seasonID int64) (int64, error) {
	var id int64
	err := c.store.DB().QueryRowContext(ctx,
		"SELECT id FROM teams WHERE key = ? AND season_id = ?",
		key, seasonID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("team %s not found in season: %w", key, err)
	}
	return id, nil
}
