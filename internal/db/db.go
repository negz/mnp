// Package db implements SQLite storage for MNP data.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite" // SQL driver registration.
)

// SQLiteStore is a SQLite database for MNP data.
type SQLiteStore struct {
	db *sql.DB
}

// Open opens or creates a SQLite database at the given path.
func Open(ctx context.Context, path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable foreign keys and WAL mode for better performance.
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL;"); err != nil {
		db.Close() //nolint:errcheck // Already returning an error.
		return nil, fmt.Errorf("set pragmas: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Close closes the database.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection for direct queries.
func (s *SQLiteStore) DB() *sql.DB {
	return s.db
}

// Init creates the database schema.
func (s *SQLiteStore) Init(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	return nil
}

// Schema returns the documented database schema.
func Schema() string {
	return schema
}

// GetMetadata retrieves a metadata value by key.
func (s *SQLiteStore) GetMetadata(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM sync_metadata WHERE key = ?", key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get metadata %s: %w", key, err)
	}
	return value, nil
}

// SetMetadata stores a metadata value by key.
func (s *SQLiteStore) SetMetadata(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT OR REPLACE INTO sync_metadata (key, value) VALUES (?, ?)",
		key, value)
	if err != nil {
		return fmt.Errorf("set metadata %s: %w", key, err)
	}
	return nil
}

// LoadedSeasons returns season numbers that have at least one match loaded.
func (s *SQLiteStore) LoadedSeasons(ctx context.Context) (map[int]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT s.number
		FROM seasons s
		JOIN matches m ON m.season_id = s.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	seasons := make(map[int]bool)
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		seasons[n] = true
	}
	return seasons, rows.Err()
}

// MaxSeasonNumber returns the highest season number in the database, or 0 if none.
func (s *SQLiteStore) MaxSeasonNumber(ctx context.Context) (int, error) {
	var n sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT MAX(number) FROM seasons").Scan(&n)
	if err != nil {
		return 0, err
	}
	return int(n.Int64), nil
}

const schema = `
-- Pinball machines (from MNP + IPDB metadata)
--
-- Example: key='TAF', name='The Addams Family', manufacturer='Williams', year=1992
CREATE TABLE IF NOT EXISTS machines (
    key TEXT PRIMARY KEY,           -- MNP's short code (e.g., 'TAF', 'MM', 'TZ')
    name TEXT NOT NULL,             -- Full name (e.g., 'The Addams Family')
    manufacturer TEXT,              -- 'Williams', 'Bally', 'Stern', etc.
    year INTEGER,                   -- Year released
    type TEXT,                      -- 'SS' (solid state) or 'EM' (electromechanical)
    ipdb_id INTEGER                 -- Internet Pinball Database ID
);

-- League seasons
CREATE TABLE IF NOT EXISTS seasons (
    id INTEGER PRIMARY KEY,
    number INTEGER NOT NULL UNIQUE  -- Season number (14-23+)
);

-- Players (deduplicated across seasons by name)
CREATE TABLE IF NOT EXISTS players (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE       -- Player's display name
);

-- Teams (per season - team keys can repeat across seasons)
--
-- Example: key='CRA', name='Castle Crashers', season_id=10
CREATE TABLE IF NOT EXISTS teams (
    id INTEGER PRIMARY KEY,
    key TEXT NOT NULL,              -- Short code (e.g., 'CRA', 'PYC')
    name TEXT NOT NULL,             -- Full name (e.g., 'Castle Crashers')
    season_id INTEGER NOT NULL REFERENCES seasons(id),
    home_venue_id INTEGER REFERENCES venues(id),
    UNIQUE(key, season_id)
);

-- Venues (pinball bars/arcades)
--
-- Example: key='ANC', name='Add-a-Ball'
CREATE TABLE IF NOT EXISTS venues (
    id INTEGER PRIMARY KEY,
    key TEXT NOT NULL UNIQUE,       -- Short code (e.g., 'ANC', 'SAM')
    name TEXT NOT NULL              -- Full name (e.g., 'Add-a-Ball')
);

-- Machines available at each venue per season
CREATE TABLE IF NOT EXISTS venue_machines (
    venue_id INTEGER NOT NULL REFERENCES venues(id),
    machine_key TEXT NOT NULL REFERENCES machines(key),
    season_id INTEGER NOT NULL REFERENCES seasons(id),
    PRIMARY KEY (venue_id, machine_key, season_id)
);

-- Rosters (player-team membership)
CREATE TABLE IF NOT EXISTS rosters (
    player_id INTEGER NOT NULL REFERENCES players(id),
    team_id INTEGER NOT NULL REFERENCES teams(id),
    role TEXT NOT NULL DEFAULT 'P', -- 'C' (captain), 'A' (assistant), 'P' (player)
    PRIMARY KEY (player_id, team_id)
);

-- Matches between teams
--
-- Example: key='mnp-23-1-CRA-PYC' (season 23, week 1, CRA vs PYC)
CREATE TABLE IF NOT EXISTS matches (
    id INTEGER PRIMARY KEY,
    key TEXT NOT NULL UNIQUE,       -- e.g., 'mnp-23-1-CRA-PYC'
    season_id INTEGER NOT NULL REFERENCES seasons(id),
    week INTEGER NOT NULL,          -- Week number within season
    date TEXT,                      -- ISO date (e.g., '2024-01-15')
    home_team_id INTEGER NOT NULL REFERENCES teams(id),
    away_team_id INTEGER NOT NULL REFERENCES teams(id),
    venue_id INTEGER REFERENCES venues(id),
    home_points INTEGER,            -- Total points (max 82), NULL if not played
    away_points INTEGER             -- Total points (max 82), NULL if not played
);

-- Individual games within a match
--
-- Each match has 4 rounds: doubles (R1) -> singles (R2, R3) -> doubles (R4)
CREATE TABLE IF NOT EXISTS games (
    id INTEGER PRIMARY KEY,
    match_id INTEGER NOT NULL REFERENCES matches(id),
    round INTEGER NOT NULL,         -- 1-4 (rounds 1 and 4 are doubles)
    machine_key TEXT,               -- References machines(key), may not exist yet
    is_doubles INTEGER NOT NULL DEFAULT 0
);

-- Player results for each game
--
-- Points are stored as 2x actual values to handle half-points as integers.
-- Doubles: 5 points possible (stored as 10)
-- Singles: 3 points possible (stored as 6)
CREATE TABLE IF NOT EXISTS game_results (
    game_id INTEGER NOT NULL REFERENCES games(id),
    player_id INTEGER NOT NULL REFERENCES players(id),
    team_id INTEGER NOT NULL REFERENCES teams(id),
    position INTEGER NOT NULL,      -- Player order (1-4 for doubles, 1-2 for singles)
    score INTEGER,                  -- Pinball score achieved
    points_won INTEGER NOT NULL,    -- League points * 2 (e.g., 3 points = 6)
    points_possible INTEGER NOT NULL, -- Max points * 2 (10 for doubles, 6 for singles)
    PRIMARY KEY (game_id, player_id)
);

-- Indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_game_results_player ON game_results(player_id);
CREATE INDEX IF NOT EXISTS idx_game_results_machine ON game_results(game_id);
CREATE INDEX IF NOT EXISTS idx_games_machine ON games(machine_key);
CREATE INDEX IF NOT EXISTS idx_matches_season ON matches(season_id);
CREATE INDEX IF NOT EXISTS idx_teams_season ON teams(season_id);

-- Sync metadata for tracking cache freshness
CREATE TABLE IF NOT EXISTS sync_metadata (
    key TEXT PRIMARY KEY,            -- e.g., 'ipdb_last_sync'
    value TEXT NOT NULL              -- ISO timestamp or other value
);
`
