package db

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/negz/mnp/internal/recency"
)

// TeamSummary contains team info for display, including venue name.
type TeamSummary struct {
	Key   string
	Name  string
	Venue string
}

// ListMachines returns machines that have been played, optionally filtered by a
// case-insensitive search term matching key or name.
func (s *SQLiteStore) ListMachines(ctx context.Context, search string) ([]Machine, error) {
	query := `
		SELECT m.key, m.name
		FROM machines m
		WHERE m.key IN (SELECT DISTINCT machine_key FROM games WHERE machine_key IS NOT NULL)
	`
	var args []any

	if search != "" {
		query += " AND (LOWER(m.key) LIKE ? OR LOWER(m.name) LIKE ?)"
		pattern := "%" + strings.ToLower(search) + "%"
		args = append(args, pattern, pattern)
	}

	query += " ORDER BY m.key"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query machines: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var result []Machine
	for rows.Next() {
		var m Machine
		if err := rows.Scan(&m.Key, &m.Name); err != nil {
			return nil, fmt.Errorf("scan machine: %w", err)
		}
		result = append(result, m)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate machines: %w", err)
	}

	return result, nil
}

// ListVenues returns all venues, optionally filtered by a case-insensitive
// search term matching key or name.
func (s *SQLiteStore) ListVenues(ctx context.Context, search string) ([]Venue, error) {
	query := "SELECT key, name FROM venues WHERE 1=1"
	var args []any

	if search != "" {
		query += " AND (LOWER(key) LIKE ? OR LOWER(name) LIKE ?)"
		pattern := "%" + strings.ToLower(search) + "%"
		args = append(args, pattern, pattern)
	}

	query += " ORDER BY key"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query venues: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var result []Venue
	for rows.Next() {
		var v Venue
		if err := rows.Scan(&v.Key, &v.Name); err != nil {
			return nil, fmt.Errorf("scan venue: %w", err)
		}
		result = append(result, v)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate venues: %w", err)
	}

	return result, nil
}

// ListTeams returns teams in the current (latest) season, optionally filtered
// by a case-insensitive search term matching key or name.
func (s *SQLiteStore) ListTeams(ctx context.Context, search string) ([]TeamSummary, error) {
	query := `
		SELECT t.key, t.name, COALESCE(v.name || ' (' || v.key || ')', '') as venue
		FROM teams t
		LEFT JOIN venues v ON v.id = t.home_venue_id
		WHERE t.season_id = (SELECT MAX(season_id) FROM teams)
	`
	var args []any

	if search != "" {
		query += " AND (LOWER(t.key) LIKE ? OR LOWER(t.name) LIKE ?)"
		pattern := "%" + strings.ToLower(search) + "%"
		args = append(args, pattern, pattern)
	}

	query += " ORDER BY t.key"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query teams: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var result []TeamSummary
	for rows.Next() {
		var t TeamSummary
		if err := rows.Scan(&t.Key, &t.Name, &t.Venue); err != nil {
			return nil, fmt.Errorf("scan team: %w", err)
		}
		result = append(result, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate teams: %w", err)
	}

	return result, nil
}

// PlayerSummary contains player info for display, including their current team.
type PlayerSummary struct {
	Name    string
	TeamKey string
	Team    string
	IPR     int
}

// ListPlayers returns players on rosters for the current (latest) season,
// optionally filtered by a case-insensitive search term matching player name,
// team key, or team name.
func (s *SQLiteStore) ListPlayers(ctx context.Context, search string) ([]PlayerSummary, error) {
	query := `
		SELECT p.name, t.key, t.name, COALESCE(ipr.ipr, 0)
		FROM players p
		JOIN rosters r ON r.player_id = p.id
		JOIN teams t ON t.id = r.team_id
		LEFT JOIN player_iprs ipr ON ipr.name = p.name
		WHERE t.season_id = (SELECT MAX(season_id) FROM teams)
	`
	var args []any

	if search != "" {
		query += " AND (LOWER(p.name) LIKE ? OR LOWER(t.key) LIKE ? OR LOWER(t.name) LIKE ?)"
		pattern := "%" + strings.ToLower(search) + "%"
		args = append(args, pattern, pattern, pattern)
	}

	query += " ORDER BY p.name"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query players: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var result []PlayerSummary
	for rows.Next() {
		var p PlayerSummary
		if err := rows.Scan(&p.Name, &p.TeamKey, &p.Team, &p.IPR); err != nil {
			return nil, fmt.Errorf("scan player: %w", err)
		}
		result = append(result, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate players: %w", err)
	}

	return result, nil
}

// ListSubs returns players who've played for a team without being on its
// roster, and who aren't on any team's roster now, so could sub for it again.
// Those who've subbed for it the most lately come first.
func (s *SQLiteStore) ListSubs(ctx context.Context, teamKey string) ([]PlayerSummary, error) {
	latest, err := s.MaxSeasonNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("get latest season: %w", err)
	}

	// Starting from the team's matches, rather than every game result, lets
	// SQLite use the index on each game's results.
	rows, err := s.db.QueryContext(ctx, `
		WITH team AS (
			SELECT id FROM teams WHERE key = ?
		),
		rostered AS (
			SELECT r.player_id FROM rosters r
			JOIN teams t ON t.id = r.team_id
			WHERE t.season_id = (
				SELECT s.id FROM seasons s
				WHERE EXISTS (SELECT 1 FROM teams t WHERE t.season_id = s.id)
				ORDER BY s.number DESC LIMIT 1
			)
		),
		played AS (
			SELECT id, season_id FROM matches
			WHERE home_team_id IN team OR away_team_id IN team
		)
		SELECT p.name, COALESCE(ipr.ipr, 0), s.number, COUNT(DISTINCT m.id)
		FROM played m
		JOIN games g ON g.match_id = m.id
		JOIN game_results gr ON gr.game_id = g.id AND gr.team_id IN team
		JOIN players p ON p.id = gr.player_id
		JOIN seasons s ON s.id = m.season_id
		LEFT JOIN player_iprs ipr ON ipr.name = p.name
		LEFT JOIN rosters r ON r.player_id = gr.player_id AND r.team_id = gr.team_id
		WHERE r.player_id IS NULL
		  AND gr.player_id NOT IN rostered
		GROUP BY p.id, s.number
	`, teamKey)
	if err != nil {
		return nil, fmt.Errorf("query subs: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var subs []PlayerSummary
	matches := make(map[string]float64)
	for rows.Next() {
		var p PlayerSummary
		var season, n int
		if err := rows.Scan(&p.Name, &p.IPR, &season, &n); err != nil {
			return nil, fmt.Errorf("scan sub: %w", err)
		}
		if _, ok := matches[p.Name]; !ok {
			subs = append(subs, p)
		}
		matches[p.Name] += float64(n) * recency.Weight(season, latest, recency.PlayHalfLife)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate subs: %w", err)
	}

	slices.SortStableFunc(subs, func(a, b PlayerSummary) int {
		return cmp.Or(cmp.Compare(matches[b.Name], matches[a.Name]), cmp.Compare(a.Name, b.Name))
	})
	return subs, nil
}

// ListIPRs returns every rated player's IPR, keyed by their name.
func (s *SQLiteStore) ListIPRs(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT name, ipr FROM player_iprs")
	if err != nil {
		return nil, fmt.Errorf("query IPRs: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	iprs := make(map[string]int)
	for rows.Next() {
		var name string
		var ipr int
		if err := rows.Scan(&name, &ipr); err != nil {
			return nil, fmt.Errorf("scan IPR: %w", err)
		}
		iprs[name] = ipr
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate IPRs: %w", err)
	}
	return iprs, nil
}

// ListMachineKeys returns the keys of all known machines.
func (s *SQLiteStore) ListMachineKeys(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key FROM machines")
	if err != nil {
		return nil, fmt.Errorf("query machine keys: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	result := make(map[string]bool)
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("scan machine key: %w", err)
		}
		result[k] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate machine keys: %w", err)
	}

	return result, nil
}

// GetTeamID returns the ID of a team by key and season ID.
func (s *SQLiteStore) GetTeamID(ctx context.Context, key string, seasonID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx,
		"SELECT id FROM teams WHERE key = ? AND season_id = ?",
		key, seasonID).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("team %s not found in season: %w", key, err)
	}
	return id, nil
}

// ScheduleMatch contains match schedule info with resolved team and venue
// names for display.
type ScheduleMatch struct {
	Week        int
	Date        string
	HomeTeamKey string
	HomeTeam    string
	AwayTeamKey string
	AwayTeam    string
	VenueKey    string
	Venue       string
}

// ListSchedule returns all matches on or after the given date, ordered by week
// then date. The date should be an ISO 8601 date string (e.g. "2025-02-07").
func (s *SQLiteStore) ListSchedule(ctx context.Context, after string) ([]ScheduleMatch, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			m.week,
			m.date,
			ht.key,
			ht.name,
			at.key,
			at.name,
			COALESCE(v.key, ''),
			COALESCE(v.name, '')
		FROM matches m
		JOIN teams ht ON ht.id = m.home_team_id
		JOIN teams at ON at.id = m.away_team_id
		LEFT JOIN venues v ON v.id = m.venue_id
		WHERE m.season_id = (SELECT MAX(id) FROM seasons)
		  AND m.date >= ?
		ORDER BY m.week, m.date
	`, after)
	if err != nil {
		return nil, fmt.Errorf("query schedule: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var result []ScheduleMatch
	for rows.Next() {
		var sm ScheduleMatch
		if err := rows.Scan(
			&sm.Week,
			&sm.Date,
			&sm.HomeTeamKey,
			&sm.HomeTeam,
			&sm.AwayTeamKey,
			&sm.AwayTeam,
			&sm.VenueKey,
			&sm.Venue,
		); err != nil {
			return nil, fmt.Errorf("scan schedule match: %w", err)
		}
		result = append(result, sm)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedule: %w", err)
	}

	return result, nil
}
