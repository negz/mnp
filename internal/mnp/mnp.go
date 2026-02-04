// Package mnp syncs and loads data from the MNP data archive.
package mnp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-git/go-git/v5"

	"github.com/negz/mnp/internal/db"
)

// Sync clones or updates the MNP data archive from the given repo URL.
// If verbose is true, progress is printed to stdout.
func Sync(ctx context.Context, repoURL, path string, verbose bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	var progress io.Writer
	if verbose {
		progress = os.Stdout
	}

	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		if verbose {
			fmt.Printf("Updating MNP archive...\n")
		}
		r, err := git.PlainOpen(path)
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

	if verbose {
		fmt.Printf("Cloning MNP archive...\n")
	}
	_, err := git.PlainCloneContext(ctx, path, false, &git.CloneOptions{
		URL:          repoURL,
		Depth:        1,
		SingleBranch: true,
		Progress:     progress,
	})
	return err
}

// Load reads the MNP archive and loads all data into the store.
func Load(ctx context.Context, store *db.Store, archivePath string) error {
	if err := LoadGlobals(ctx, store, archivePath); err != nil {
		return err
	}

	// Find all season directories
	entries, err := os.ReadDir(archivePath)
	if err != nil {
		return fmt.Errorf("read archive: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "season-") {
			continue
		}

		seasonNum, err := strconv.Atoi(strings.TrimPrefix(e.Name(), "season-"))
		if err != nil {
			continue
		}

		fmt.Printf("  Loading season %d...\n", seasonNum)
		seasonPath := filepath.Join(archivePath, e.Name())
		if err := LoadSeason(ctx, store, seasonPath, seasonNum); err != nil {
			return fmt.Errorf("load season %d: %w", seasonNum, err)
		}
	}

	return nil
}

// LoadGlobals loads machines.json and venues.json from the archive root.
func LoadGlobals(ctx context.Context, store *db.Store, archivePath string) error {
	if err := loadMachines(ctx, store, archivePath); err != nil {
		return fmt.Errorf("load machines: %w", err)
	}
	if err := loadVenues(ctx, store, archivePath); err != nil {
		return fmt.Errorf("load venues: %w", err)
	}
	return nil
}

// LoadSeason loads a single season's data (teams, rosters, matches).
func LoadSeason(ctx context.Context, store *db.Store, seasonPath string, seasonNum int) error {
	return loadSeason(ctx, store, seasonPath, seasonNum)
}

func loadMachines(ctx context.Context, store *db.Store, archivePath string) error {
	f, err := os.Open(filepath.Join(archivePath, "machines.json")) //nolint:gosec // Internal archive path.
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
		if err := store.UpsertMachine(ctx, db.Machine{
			Key:  m.Key,
			Name: m.Name,
		}); err != nil {
			return err
		}
	}

	return nil
}

func loadVenues(ctx context.Context, store *db.Store, archivePath string) error {
	f, err := os.Open(filepath.Join(archivePath, "venues.json")) //nolint:gosec // Internal archive path.
	if err != nil {
		return fmt.Errorf("open venues.json: %w", err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.

	var venues map[string]struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(f).Decode(&venues); err != nil {
		return fmt.Errorf("decode venues.json: %w", err)
	}

	for _, v := range venues {
		if _, err := store.UpsertVenue(ctx, v.Key, v.Name); err != nil {
			return err
		}
	}

	return nil
}

func loadSeason(ctx context.Context, store *db.Store, seasonPath string, seasonNum int) error {
	seasonID, err := store.UpsertSeason(ctx, seasonNum)
	if err != nil {
		return err
	}

	// Load season.json for teams and rosters
	if err := loadSeasonJSON(ctx, store, seasonPath, seasonID); err != nil {
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
		if err := loadMatch(ctx, store, matchPath, seasonID); err != nil {
			fmt.Printf("    Warning: failed to load %s: %v\n", e.Name(), err)
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

func loadSeasonJSON(ctx context.Context, store *db.Store, seasonPath string, seasonID int64) error {
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
			venueID, _ = store.UpsertVenue(ctx, t.Venue, t.Venue)
		}

		teamID, err := store.UpsertTeam(ctx, db.Team{
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
			playerID, err := store.UpsertPlayer(ctx, p.Name)
			if err != nil {
				return err
			}
			if err := store.UpsertRoster(ctx, playerID, teamID, "P"); err != nil {
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

func loadMatch(ctx context.Context, store *db.Store, matchPath string, seasonID int64) error {
	f, err := os.Open(matchPath) //nolint:gosec // Internal archive path.
	if err != nil {
		return fmt.Errorf("open match file: %w", err)
	}
	defer f.Close() //nolint:errcheck // Read-only file.

	var m matchJSON
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		return fmt.Errorf("decode match: %w", err)
	}

	// Skip incomplete matches
	if m.State != "complete" {
		return nil
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
		venueID, _ = store.UpsertVenue(ctx, m.Venue.Key, m.Venue.Name)
	}

	// Get team IDs
	homeTeamID, err := getTeamID(ctx, store, m.Home.Key, seasonID)
	if err != nil {
		return fmt.Errorf("get home team: %w", err)
	}
	awayTeamID, err := getTeamID(ctx, store, m.Away.Key, seasonID)
	if err != nil {
		return fmt.Errorf("get away team: %w", err)
	}

	week, _ := strconv.Atoi(m.Week)
	matchID, err := store.UpsertMatch(ctx, db.Match{
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
	if err := store.DeleteMatchGames(ctx, matchID); err != nil {
		return err
	}

	return loadMatchGames(ctx, store, matchID, m.Rounds, homeTeamID, awayTeamID, playerNames)
}

func loadMatchGames(ctx context.Context, store *db.Store, matchID int64, rounds []roundJSON, homeTeamID, awayTeamID int64, playerNames map[string]string) error {
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

			gameID, err := store.InsertGame(ctx, db.Game{
				MatchID:    matchID,
				Round:      r.N,
				MachineKey: g.Machine,
				IsDoubles:  isDoubles,
			})
			if err != nil {
				return err
			}

			if err := insertGameResults(ctx, store, gameID, g, homeTeamID, awayTeamID, playerNames, pointsPossible, isDoubles); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertGameResults(ctx context.Context, store *db.Store, gameID int64, g gameJSON, homeTeamID, awayTeamID int64, playerNames map[string]string, pointsPossible int, isDoubles bool) error {
	for _, p := range buildPlayerResults(g, homeTeamID, awayTeamID, isDoubles) {
		if p.hash == "" {
			continue
		}

		name, ok := playerNames[p.hash]
		if !ok {
			continue // Unknown player
		}

		playerID, err := store.UpsertPlayer(ctx, name)
		if err != nil {
			return err
		}

		if err := store.InsertGameResult(ctx, db.GameResult{
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

func getTeamID(ctx context.Context, store *db.Store, key string, seasonID int64) (int64, error) {
	var id int64
	err := store.DB().QueryRowContext(ctx,
		"SELECT id FROM teams WHERE key = ? AND season_id = ?",
		key, seasonID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("team %s not found in season: %w", key, err)
	}
	return id, nil
}
