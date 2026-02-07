package db

import (
	"context"
	"fmt"
)

// PlayerStats contains aggregated stats for a player on a specific machine.
type PlayerStats struct {
	Name     string
	Games    int
	P50Score float64 // Median (50th percentile)
	P90Score float64 // 90th percentile
}

// TeamMachineStats contains aggregated stats for a team on a specific machine.
type TeamMachineStats struct {
	MachineKey string
	Games      int
	P50Score   float64 // Median (50th percentile)
	P90Score   float64 // 90th percentile
	TopPlayers []TopPlayer
}

// TopPlayer is a player's P50 score on a machine.
type TopPlayer struct {
	Name     string
	P50Score float64
}

// GetTeamMachineStats returns per-machine stats for a team's current roster.
// Stats are aggregated across all seasons, but only for players currently on
// the team (latest season with that team key).
// If venueKey is non-empty, filters to games played at that venue.
// Results are ordered by play count descending (most-played machines first).
// Top 2 players per machine (by P50) are included.
func (s *SQLiteStore) GetTeamMachineStats(ctx context.Context, teamKey, venueKey string) ([]TeamMachineStats, error) {
	stats, err := s.getTeamMachineAgg(ctx, teamKey, venueKey)
	if err != nil {
		return nil, err
	}

	topPlayers, err := s.getTopPlayers(ctx, teamKey, venueKey)
	if err != nil {
		return nil, err
	}

	for i := range stats {
		stats[i].TopPlayers = topPlayers[stats[i].MachineKey]
	}

	return stats, nil
}

// getTeamMachineAgg returns per-machine aggregate stats (P50, P90) for a
// team's current roster.
func (s *SQLiteStore) getTeamMachineAgg(ctx context.Context, teamKey, venueKey string) ([]TeamMachineStats, error) {
	query := `
		WITH current_roster AS (
			SELECT DISTINCT p.id as player_id
			FROM players p
			JOIN rosters r ON r.player_id = p.id
			JOIN teams t ON t.id = r.team_id
			WHERE t.key = ?
			  AND t.season_id = (
				SELECT MAX(t2.season_id) FROM teams t2 WHERE t2.key = ?
			  )
		),
		scores AS (
			SELECT
				g.machine_key,
				gr.score,
				ROW_NUMBER() OVER (PARTITION BY g.machine_key ORDER BY gr.score) as rn,
				COUNT(*) OVER (PARTITION BY g.machine_key) as total
			FROM game_results gr
			JOIN players p ON p.id = gr.player_id
			JOIN games g ON g.id = gr.game_id
			JOIN matches m ON m.id = g.match_id
			WHERE p.id IN (SELECT player_id FROM current_roster)
			  AND g.machine_key IS NOT NULL
	`
	args := []any{teamKey, teamKey}

	if venueKey != "" {
		query += " AND m.venue_id = (SELECT id FROM venues WHERE key = ?)"
		query += ` AND g.machine_key IN (
			SELECT vm.machine_key FROM venue_machines vm
			JOIN venues v ON v.id = vm.venue_id
			WHERE v.key = ?)`
		args = append(args, venueKey, venueKey)
	}

	query += `
		),
		machine_agg AS (
			SELECT DISTINCT machine_key, total
			FROM scores
		)
		SELECT
			ma.machine_key,
			ma.total as games,
			(SELECT score FROM scores s WHERE s.machine_key = ma.machine_key
			 AND s.rn = (ma.total + 1) / 2) as p50,
			(SELECT score FROM scores s WHERE s.machine_key = ma.machine_key
			 AND s.rn = (ma.total * 9 + 9) / 10) as p90
		FROM machine_agg ma
		ORDER BY games DESC
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query team machine stats: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var stats []TeamMachineStats
	for rows.Next() {
		var ts TeamMachineStats
		if err := rows.Scan(&ts.MachineKey, &ts.Games, &ts.P50Score, &ts.P90Score); err != nil {
			return nil, fmt.Errorf("scan team machine stats: %w", err)
		}
		stats = append(stats, ts)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate team machine stats: %w", err)
	}

	return stats, nil
}

// getTopPlayers returns the top 2 players by P50 score for each machine,
// keyed by machine key.
func (s *SQLiteStore) getTopPlayers(ctx context.Context, teamKey, venueKey string) (map[string][]TopPlayer, error) {
	query := `
		WITH current_roster AS (
			SELECT DISTINCT p.id as player_id
			FROM players p
			JOIN rosters r ON r.player_id = p.id
			JOIN teams t ON t.id = r.team_id
			WHERE t.key = ?
			  AND t.season_id = (
				SELECT MAX(t2.season_id) FROM teams t2 WHERE t2.key = ?
			  )
		),
		player_scores AS (
			SELECT
				g.machine_key,
				gr.player_id,
				p.name,
				gr.score,
				ROW_NUMBER() OVER (PARTITION BY g.machine_key, gr.player_id ORDER BY gr.score) as rn,
				COUNT(*) OVER (PARTITION BY g.machine_key, gr.player_id) as total
			FROM game_results gr
			JOIN players p ON p.id = gr.player_id
			JOIN games g ON g.id = gr.game_id
			JOIN matches m ON m.id = g.match_id
			WHERE p.id IN (SELECT player_id FROM current_roster)
			  AND g.machine_key IS NOT NULL
	`
	args := []any{teamKey, teamKey}

	if venueKey != "" {
		query += " AND m.venue_id = (SELECT id FROM venues WHERE key = ?)"
		query += ` AND g.machine_key IN (
			SELECT vm.machine_key FROM venue_machines vm
			JOIN venues v ON v.id = vm.venue_id
			WHERE v.key = ?)`
		args = append(args, venueKey, venueKey)
	}

	query += `
		),
		player_p50 AS (
			SELECT DISTINCT
				machine_key,
				player_id,
				name,
				(SELECT score FROM player_scores ps2
				 WHERE ps2.machine_key = ps1.machine_key
				   AND ps2.player_id = ps1.player_id
				   AND ps2.rn = (ps1.total + 1) / 2
				) as p50
			FROM player_scores ps1
		),
		ranked AS (
			SELECT
				machine_key,
				name,
				p50,
				ROW_NUMBER() OVER (PARTITION BY machine_key ORDER BY p50 DESC) as rn
			FROM player_p50
		)
		SELECT machine_key, name, p50
		FROM ranked
		WHERE rn <= 2
		ORDER BY machine_key, rn
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query top players: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	result := make(map[string][]TopPlayer)
	for rows.Next() {
		var machineKey, name string
		var p50 float64
		if err := rows.Scan(&machineKey, &name, &p50); err != nil {
			return nil, fmt.Errorf("scan top player: %w", err)
		}
		result[machineKey] = append(result[machineKey], TopPlayer{Name: name, P50Score: p50})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top players: %w", err)
	}

	return result, nil
}

// GetLeagueP50 returns the league-wide P50 score for each machine. League P50
// is computed across all scores by players on any team's current roster.
func (s *SQLiteStore) GetLeagueP50(ctx context.Context) (map[string]float64, error) {
	query := `
		WITH current_roster_all AS (
			SELECT DISTINCT r.player_id
			FROM rosters r
			JOIN teams t ON t.id = r.team_id
			WHERE t.season_id = (SELECT MAX(season_id) FROM teams)
		),
		scores AS (
			SELECT
				g.machine_key,
				gr.score,
				ROW_NUMBER() OVER (PARTITION BY g.machine_key ORDER BY gr.score) as rn,
				COUNT(*) OVER (PARTITION BY g.machine_key) as total
			FROM game_results gr
			JOIN players p ON p.id = gr.player_id
			JOIN games g ON g.id = gr.game_id
			WHERE p.id IN (SELECT player_id FROM current_roster_all)
			  AND g.machine_key IS NOT NULL
		),
		machine_agg AS (
			SELECT DISTINCT machine_key, total
			FROM scores
		)
		SELECT
			ma.machine_key,
			(SELECT score FROM scores s WHERE s.machine_key = ma.machine_key
			 AND s.rn = (ma.total + 1) / 2) as p50
		FROM machine_agg ma
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query league P50: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	result := make(map[string]float64)
	for rows.Next() {
		var key string
		var p50 float64
		if err := rows.Scan(&key, &p50); err != nil {
			return nil, fmt.Errorf("scan league P50: %w", err)
		}
		result[key] = p50
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate league P50: %w", err)
	}

	return result, nil
}

// GetPlayerMachineStats returns stats for players on a team's current roster.
// Stats are aggregated across all seasons, but only for players currently on
// the team (latest season with that team key).
// If venueKey is non-empty, filters to games played at that venue.
// Results are ordered by P50 score descending.
func (s *SQLiteStore) GetPlayerMachineStats(ctx context.Context, teamKey, machineKey, venueKey string) ([]PlayerStats, error) {
	query := `
		WITH current_roster AS (
			SELECT DISTINCT p.id as player_id
			FROM players p
			JOIN rosters r ON r.player_id = p.id
			JOIN teams t ON t.id = r.team_id
			WHERE t.key = ?
			  AND t.season_id = (
				SELECT MAX(t2.season_id) FROM teams t2 WHERE t2.key = ?
			  )
		),
		player_scores AS (
			SELECT
				p.id as player_id,
				p.name,
				gr.score,
				ROW_NUMBER() OVER (PARTITION BY p.id ORDER BY gr.score) as rn,
				COUNT(*) OVER (PARTITION BY p.id) as total
			FROM game_results gr
			JOIN players p ON p.id = gr.player_id
			JOIN games g ON g.id = gr.game_id
			JOIN matches m ON m.id = g.match_id
			WHERE g.machine_key = ?
			  AND p.id IN (SELECT player_id FROM current_roster)
	`
	args := []any{teamKey, teamKey, machineKey}

	if venueKey != "" {
		query += " AND m.venue_id = (SELECT id FROM venues WHERE key = ?)"
		args = append(args, venueKey)
	}

	query += `
		),
		player_agg AS (
			SELECT DISTINCT player_id, name, total
			FROM player_scores
		)
		SELECT
			pa.name,
			pa.total as games,
			(SELECT score FROM player_scores ps WHERE ps.player_id = pa.player_id
			 AND ps.rn = (pa.total + 1) / 2) as p50,
			(SELECT score FROM player_scores ps WHERE ps.player_id = pa.player_id
			 AND ps.rn = (pa.total * 9 + 9) / 10) as p90
		FROM player_agg pa
		ORDER BY p50 DESC
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query player stats: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var stats []PlayerStats
	for rows.Next() {
		var ps PlayerStats
		if err := rows.Scan(&ps.Name, &ps.Games, &ps.P50Score, &ps.P90Score); err != nil {
			return nil, fmt.Errorf("scan player stats: %w", err)
		}
		stats = append(stats, ps)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate player stats: %w", err)
	}

	return stats, nil
}
