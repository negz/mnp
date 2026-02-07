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
	P75Score float64 // 75th percentile
	MaxScore int64
}

// GetPlayerMachineStats returns stats for players on a team's current roster.
// Stats are aggregated across all seasons, but only for players currently on
// the team (latest season with that team key).
// If venueKey is non-empty, filters to games played at that venue.
// Results are ordered by P50 score descending.
func (s *SQLiteStore) GetPlayerMachineStats(ctx context.Context, teamKey, machineKey, venueKey string) ([]PlayerStats, error) {
	// Get current roster players (from latest season with this team key).
	// Then get their stats across all time, regardless of which team they
	// played for when they got those stats.
	//
	// Uses window functions to compute percentiles:
	// 1. ROW_NUMBER orders each player's scores
	// 2. COUNT gives total games per player
	// 3. Correlated subqueries select P50 and P75 positions
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
			 AND ps.rn = (pa.total * 3 + 3) / 4) as p75,
			(SELECT MAX(score) FROM player_scores ps WHERE ps.player_id = pa.player_id) as max_score
		FROM player_agg pa
		ORDER BY p75 DESC
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query player stats: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	var stats []PlayerStats
	for rows.Next() {
		var ps PlayerStats
		if err := rows.Scan(&ps.Name, &ps.Games, &ps.P50Score, &ps.P75Score, &ps.MaxScore); err != nil {
			return nil, fmt.Errorf("scan player stats: %w", err)
		}
		stats = append(stats, ps)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate player stats: %w", err)
	}

	return stats, nil
}
