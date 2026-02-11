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
	IPR      int
}

// TeamMachineStats contains aggregated stats for a team on a specific machine.
type TeamMachineStats struct {
	MachineKey    string
	Games         int
	P50Score      float64 // Median (50th percentile)
	P90Score      float64 // 90th percentile
	LikelyPlayers []LikelyPlayer
}

// LikelyPlayer is a player likely to play a machine, ranked by play count.
type LikelyPlayer struct {
	Name     string
	Games    int
	P50Score float64
}

// GetTeamMachineStats returns per-machine stats for a team's current roster.
// Stats are aggregated across all seasons, but only for players currently on
// the team (latest season with that team key).
// If venueKey is non-empty, filters to games played at that venue.
// Results are ordered by play count descending (most-played machines first).
// Top 2 players per machine (by P50) are included.
func (s *SQLiteStore) GetTeamMachineStats(ctx context.Context, teamKey, venueKey string) ([]TeamMachineStats, error) {
	stats, err := s.GetTeamMachineAgg(ctx, teamKey, venueKey)
	if err != nil {
		return nil, err
	}

	topPlayers, err := s.GetTopPlayers(ctx, teamKey, venueKey)
	if err != nil {
		return nil, err
	}

	for i := range stats {
		stats[i].LikelyPlayers = topPlayers[stats[i].MachineKey]
	}

	return stats, nil
}

// GetTeamMachineAgg returns per-machine aggregate stats (P50, P90) for a
// team's current roster.
func (s *SQLiteStore) GetTeamMachineAgg(ctx context.Context, teamKey, venueKey string) ([]TeamMachineStats, error) {
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

// GetTopPlayers returns the top 2 players by play count for each machine,
// keyed by machine key.
func (s *SQLiteStore) GetTopPlayers(ctx context.Context, teamKey, venueKey string) (map[string][]LikelyPlayer, error) {
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
		player_agg AS (
			SELECT DISTINCT
				machine_key,
				player_id,
				name,
				total as games,
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
				games,
				p50,
				ROW_NUMBER() OVER (PARTITION BY machine_key ORDER BY games DESC, p50 DESC) as rn
			FROM player_agg
		)
		SELECT machine_key, name, games, p50
		FROM ranked
		WHERE rn <= 2
		ORDER BY machine_key, rn
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query top players: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	result := make(map[string][]LikelyPlayer)
	for rows.Next() {
		var machineKey, name string
		var games int
		var p50 float64
		if err := rows.Scan(&machineKey, &name, &games, &p50); err != nil {
			return nil, fmt.Errorf("scan top player: %w", err)
		}
		result[machineKey] = append(result[machineKey], LikelyPlayer{Name: name, Games: games, P50Score: p50})
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

// PlayerMachineStats contains per-machine stats for a single player.
type PlayerMachineStats struct {
	MachineKey string
	Games      int
	P50Score   float64
	P90Score   float64
}

// GetSinglePlayerMachineStats returns per-machine stats for a single player.
// If venueKey is non-empty, filters to games played at that venue.
// Results are ordered by play count descending.
func (s *SQLiteStore) GetSinglePlayerMachineStats(ctx context.Context, playerName, venueKey string) ([]PlayerMachineStats, error) {
	query := `
		WITH scores AS (
			SELECT
				g.machine_key,
				gr.score,
				ROW_NUMBER() OVER (PARTITION BY g.machine_key ORDER BY gr.score) as rn,
				COUNT(*) OVER (PARTITION BY g.machine_key) as total
			FROM game_results gr
			JOIN players p ON p.id = gr.player_id
			JOIN games g ON g.id = gr.game_id
			JOIN matches m ON m.id = g.match_id
			WHERE p.name = ?
			  AND g.machine_key IS NOT NULL
	`
	args := []any{playerName}

	if venueKey != "" {
		query += " AND m.venue_id = (SELECT id FROM venues WHERE key = ?)"
		args = append(args, venueKey)
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
		return nil, fmt.Errorf("query single player machine stats: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var stats []PlayerMachineStats
	for rows.Next() {
		var ps PlayerMachineStats
		if err := rows.Scan(&ps.MachineKey, &ps.Games, &ps.P50Score, &ps.P90Score); err != nil {
			return nil, fmt.Errorf("scan single player machine stats: %w", err)
		}
		stats = append(stats, ps)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate single player machine stats: %w", err)
	}

	return stats, nil
}

// PlayerTeam contains a player's current team information.
type PlayerTeam struct {
	TeamKey  string
	TeamName string
	IPR      int
}

// GetPlayerTeam returns the current team for a player (latest season).
func (s *SQLiteStore) GetPlayerTeam(ctx context.Context, playerName string) (PlayerTeam, error) {
	var pt PlayerTeam
	err := s.db.QueryRowContext(ctx, `
		SELECT t.key, t.name, COALESCE(ipr.ipr, 0)
		FROM teams t
		JOIN rosters r ON r.team_id = t.id
		JOIN players p ON p.id = r.player_id
		LEFT JOIN player_iprs ipr ON ipr.name = p.name
		WHERE p.name = ?
		ORDER BY t.season_id DESC
		LIMIT 1
	`, playerName).Scan(&pt.TeamKey, &pt.TeamName, &pt.IPR)
	if err != nil {
		return pt, fmt.Errorf("get player team: %w", err)
	}
	return pt, nil
}

// GetMachineNames returns a map of machine key to machine name for all machines.
func (s *SQLiteStore) GetMachineNames(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT key, name FROM machines")
	if err != nil {
		return nil, fmt.Errorf("query machine names: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	result := make(map[string]string)
	for rows.Next() {
		var key, name string
		if err := rows.Scan(&key, &name); err != nil {
			return nil, fmt.Errorf("scan machine name: %w", err)
		}
		result[key] = name
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate machine names: %w", err)
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
				COALESCE(pipr.ipr, 0) as ipr,
				gr.score,
				ROW_NUMBER() OVER (PARTITION BY p.id ORDER BY gr.score) as rn,
				COUNT(*) OVER (PARTITION BY p.id) as total
			FROM game_results gr
			JOIN players p ON p.id = gr.player_id
			LEFT JOIN player_iprs pipr ON pipr.name = p.name
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
			SELECT DISTINCT player_id, name, ipr, total
			FROM player_scores
		)
		SELECT
			pa.name,
			pa.total as games,
			(SELECT score FROM player_scores ps WHERE ps.player_id = pa.player_id
			 AND ps.rn = (pa.total + 1) / 2) as p50,
			(SELECT score FROM player_scores ps WHERE ps.player_id = pa.player_id
			 AND ps.rn = (pa.total * 9 + 9) / 10) as p90,
			pa.ipr
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
		if err := rows.Scan(&ps.Name, &ps.Games, &ps.P50Score, &ps.P90Score, &ps.IPR); err != nil {
			return nil, fmt.Errorf("scan player stats: %w", err)
		}
		stats = append(stats, ps)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate player stats: %w", err)
	}

	return stats, nil
}
