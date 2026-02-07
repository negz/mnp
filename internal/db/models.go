// Package db implements SQLite storage for MNP data.
package db

import (
	"context"
	"fmt"
)

// Machine represents a pinball machine.
type Machine struct {
	Key          string
	Name         string
	Manufacturer string
	Year         int
	Type         string
	IPDBID       int
}

// UpsertMachine inserts or updates a machine.
func (s *SQLiteStore) UpsertMachine(ctx context.Context, m Machine) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO machines (key, name, manufacturer, year, type, ipdb_id)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			name = excluded.name,
			manufacturer = excluded.manufacturer,
			year = excluded.year,
			type = excluded.type,
			ipdb_id = excluded.ipdb_id
	`, m.Key, m.Name, m.Manufacturer, m.Year, m.Type, m.IPDBID); err != nil {
		return fmt.Errorf("upsert machine %s: %w", m.Key, err)
	}
	return nil
}

// Season represents a league season.
type Season struct {
	ID     int64
	Number int
}

// UpsertSeason inserts or updates a season and returns its ID.
func (s *SQLiteStore) UpsertSeason(ctx context.Context, number int) (int64, error) {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO seasons (number) VALUES (?)
		ON CONFLICT(number) DO NOTHING
	`, number); err != nil {
		return 0, fmt.Errorf("upsert season %d: %w", number, err)
	}

	var id int64
	if err := s.db.QueryRowContext(ctx, "SELECT id FROM seasons WHERE number = ?", number).Scan(&id); err != nil {
		return 0, fmt.Errorf("get season id: %w", err)
	}
	return id, nil
}

// Player represents a league player.
type Player struct {
	ID   int64
	Name string
}

// UpsertPlayer inserts or updates a player and returns their ID.
func (s *SQLiteStore) UpsertPlayer(ctx context.Context, name string) (int64, error) {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO players (name) VALUES (?)
		ON CONFLICT(name) DO NOTHING
	`, name); err != nil {
		return 0, fmt.Errorf("upsert player %s: %w", name, err)
	}

	var id int64
	if err := s.db.QueryRowContext(ctx, "SELECT id FROM players WHERE name = ?", name).Scan(&id); err != nil {
		return 0, fmt.Errorf("get player id: %w", err)
	}
	return id, nil
}

// Venue represents a pinball venue.
type Venue struct {
	ID   int64
	Key  string
	Name string
}

// UpsertVenue inserts or updates a venue and returns its ID.
func (s *SQLiteStore) UpsertVenue(ctx context.Context, key, name string) (int64, error) {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO venues (key, name) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET name = excluded.name
	`, key, name); err != nil {
		return 0, fmt.Errorf("upsert venue %s: %w", key, err)
	}

	var id int64
	if err := s.db.QueryRowContext(ctx, "SELECT id FROM venues WHERE key = ?", key).Scan(&id); err != nil {
		return 0, fmt.Errorf("get venue id: %w", err)
	}
	return id, nil
}

// Team represents a league team.
type Team struct {
	ID          int64
	Key         string
	Name        string
	SeasonID    int64
	HomeVenueID int64
}

// UpsertTeam inserts or updates a team and returns its ID.
func (s *SQLiteStore) UpsertTeam(ctx context.Context, t Team) (int64, error) {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO teams (key, name, season_id, home_venue_id)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(key, season_id) DO UPDATE SET
			name = excluded.name,
			home_venue_id = excluded.home_venue_id
	`, t.Key, t.Name, t.SeasonID, t.HomeVenueID); err != nil {
		return 0, fmt.Errorf("upsert team %s: %w", t.Key, err)
	}

	var id int64
	if err := s.db.QueryRowContext(ctx,
		"SELECT id FROM teams WHERE key = ? AND season_id = ?",
		t.Key, t.SeasonID).Scan(&id); err != nil {
		return 0, fmt.Errorf("get team id: %w", err)
	}
	return id, nil
}

// UpsertVenueMachine associates a machine with a venue.
func (s *SQLiteStore) UpsertVenueMachine(ctx context.Context, venueID int64, machineKey string) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO venue_machines (venue_id, machine_key)
		VALUES (?, ?)
		ON CONFLICT(venue_id, machine_key) DO NOTHING
	`, venueID, machineKey); err != nil {
		return fmt.Errorf("upsert venue machine %s: %w", machineKey, err)
	}
	return nil
}

// GetVenueMachines returns the machine keys at a venue.
func (s *SQLiteStore) GetVenueMachines(ctx context.Context, venueKey string) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT vm.machine_key
		FROM venue_machines vm
		JOIN venues v ON v.id = vm.venue_id
		WHERE v.key = ?
	`, venueKey)
	if err != nil {
		return nil, fmt.Errorf("query venue machines: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	result := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan venue machine: %w", err)
		}
		result[key] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate venue machines: %w", err)
	}

	return result, nil
}

// UpsertRoster adds a player to a team roster.
func (s *SQLiteStore) UpsertRoster(ctx context.Context, playerID, teamID int64, role string) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO rosters (player_id, team_id, role)
		VALUES (?, ?, ?)
		ON CONFLICT(player_id, team_id) DO UPDATE SET role = excluded.role
	`, playerID, teamID, role); err != nil {
		return fmt.Errorf("upsert roster: %w", err)
	}
	return nil
}

// Match represents a league match.
type Match struct {
	ID         int64
	Key        string
	SeasonID   int64
	Week       int
	Date       string
	HomeTeamID int64
	AwayTeamID int64
	VenueID    int64
	HomePoints int
	AwayPoints int
}

// UpsertMatch inserts or updates a match and returns its ID.
func (s *SQLiteStore) UpsertMatch(ctx context.Context, m Match) (int64, error) {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO matches (key, season_id, week, date, home_team_id, away_team_id, venue_id, home_points, away_points)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			week = excluded.week,
			date = excluded.date,
			home_team_id = excluded.home_team_id,
			away_team_id = excluded.away_team_id,
			venue_id = excluded.venue_id,
			home_points = excluded.home_points,
			away_points = excluded.away_points
	`, m.Key, m.SeasonID, m.Week, m.Date, m.HomeTeamID, m.AwayTeamID, m.VenueID, m.HomePoints, m.AwayPoints); err != nil {
		return 0, fmt.Errorf("upsert match %s: %w", m.Key, err)
	}

	var id int64
	if err := s.db.QueryRowContext(ctx, "SELECT id FROM matches WHERE key = ?", m.Key).Scan(&id); err != nil {
		return 0, fmt.Errorf("get match id: %w", err)
	}
	return id, nil
}

// Game represents an individual game within a match.
type Game struct {
	ID         int64
	MatchID    int64
	Round      int
	MachineKey string
	IsDoubles  bool
}

// InsertGame inserts a game and returns its ID.
func (s *SQLiteStore) InsertGame(ctx context.Context, g Game) (int64, error) {
	isDoubles := 0
	if g.IsDoubles {
		isDoubles = 1
	}

	result, err := s.db.ExecContext(ctx, `
		INSERT INTO games (match_id, round, machine_key, is_doubles)
		VALUES (?, ?, ?, ?)
	`, g.MatchID, g.Round, g.MachineKey, isDoubles)
	if err != nil {
		return 0, fmt.Errorf("insert game: %w", err)
	}
	return result.LastInsertId()
}

// GameResult represents a player's result in a game.
type GameResult struct {
	GameID         int64
	PlayerID       int64
	TeamID         int64
	Position       int
	Score          int64
	PointsWon      int
	PointsPossible int
}

// InsertGameResult inserts a game result.
func (s *SQLiteStore) InsertGameResult(ctx context.Context, r GameResult) error {
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO game_results (game_id, player_id, team_id, position, score, points_won, points_possible)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(game_id, player_id) DO UPDATE SET
			team_id = excluded.team_id,
			position = excluded.position,
			score = excluded.score,
			points_won = excluded.points_won,
			points_possible = excluded.points_possible
	`, r.GameID, r.PlayerID, r.TeamID, r.Position, r.Score, r.PointsWon, r.PointsPossible); err != nil {
		return fmt.Errorf("insert game result: %w", err)
	}
	return nil
}

// DeleteMatchGames deletes all games and results for a match (for re-import).
func (s *SQLiteStore) DeleteMatchGames(ctx context.Context, matchID int64) error {
	if _, err := s.db.ExecContext(ctx, `
		DELETE FROM game_results WHERE game_id IN (SELECT id FROM games WHERE match_id = ?)
	`, matchID); err != nil {
		return fmt.Errorf("delete game results: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, "DELETE FROM games WHERE match_id = ?", matchID); err != nil {
		return fmt.Errorf("delete games: %w", err)
	}
	return nil
}
