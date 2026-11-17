package db

import (
	"cmp"
	"context"
	"fmt"
	"slices"
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

// LikelyPlayer is a player likely to play a machine, ranked by how much they
// have played it lately.
type LikelyPlayer struct {
	Name     string
	Games    int
	P50Score float64
	P90Score float64
	IPR      int
}

// GetTeamMachineStats returns per-machine stats for a team's current roster.
// Stats are aggregated across all seasons, but only for players currently on
// the team (latest season with that team key). Recent seasons count for more.
// If venueKey is non-empty, filters to games played at that venue.
// Results are ordered by play count descending (most-played machines first).
// The 3 players who've played each machine the most lately are included.
func (s *SQLiteStore) GetTeamMachineStats(ctx context.Context, teamKey, venueKey string) ([]TeamMachineStats, error) {
	latest, err := s.MaxSeasonNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("get latest season: %w", err)
	}

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
		)
		SELECT g.machine_key, p.name, COALESCE(ipr.ipr, 0), s.number, gr.score
		FROM game_results gr
		JOIN players p ON p.id = gr.player_id
		LEFT JOIN player_iprs ipr ON ipr.name = p.name
		JOIN games g ON g.id = gr.game_id
		JOIN matches m ON m.id = g.match_id
		JOIN seasons s ON s.id = m.season_id
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

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query team machine stats: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	type player struct {
		ipr   int
		games []scoredGame
	}
	machines := make(map[string][]scoredGame)
	players := make(map[string]map[string]*player)
	for rows.Next() {
		var machine, name string
		var ipr int
		var g scoredGame
		if err := rows.Scan(&machine, &name, &ipr, &g.season, &g.score); err != nil {
			return nil, fmt.Errorf("scan team machine stats: %w", err)
		}
		machines[machine] = append(machines[machine], g)
		if players[machine] == nil {
			players[machine] = make(map[string]*player)
		}
		if players[machine][name] == nil {
			players[machine][name] = &player{ipr: ipr}
		}
		players[machine][name].games = append(players[machine][name].games, g)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate team machine stats: %w", err)
	}

	stats := make([]TeamMachineStats, 0, len(machines))
	for machine, games := range machines {
		type ranked struct {
			LikelyPlayer

			recent float64
		}
		likely := make([]ranked, 0, len(players[machine]))
		for name, p := range players[machine] {
			likely = append(likely, ranked{
				LikelyPlayer: LikelyPlayer{
					Name:     name,
					Games:    len(p.games),
					P50Score: percentile(p.games, latest, 0.5),
					P90Score: percentile(p.games, latest, 0.9),
					IPR:      p.ipr,
				},
				recent: plays(p.games, latest),
			})
		}
		slices.SortFunc(likely, func(a, b ranked) int {
			return cmp.Or(
				cmp.Compare(b.recent, a.recent),
				cmp.Compare(b.P50Score, a.P50Score),
				cmp.Compare(a.Name, b.Name),
			)
		})

		ts := TeamMachineStats{
			MachineKey: machine,
			Games:      len(games),
			P50Score:   percentile(games, latest, 0.5),
			P90Score:   percentile(games, latest, 0.9),
		}
		for _, r := range likely[:min(3, len(likely))] {
			ts.LikelyPlayers = append(ts.LikelyPlayers, r.LikelyPlayer)
		}
		stats = append(stats, ts)
	}

	slices.SortFunc(stats, func(a, b TeamMachineStats) int {
		return cmp.Or(cmp.Compare(b.Games, a.Games), cmp.Compare(a.MachineKey, b.MachineKey))
	})

	return stats, nil
}

// GetRosterPicks returns how many games each machine was picked for by
// players on a team's current roster, keyed by machine key. It counts picks
// from every season, including those made while playing for other teams. The
// picking team's players take positions 1 and 3.
func (s *SQLiteStore) GetRosterPicks(ctx context.Context, teamKey string) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		WITH current_roster AS (
			SELECT DISTINCT r.player_id
			FROM rosters r
			JOIN teams t ON t.id = r.team_id
			WHERE t.key = ?
			  AND t.season_id = (
				SELECT MAX(t2.season_id) FROM teams t2 WHERE t2.key = ?
			  )
		)
		SELECT g.machine_key, COUNT(DISTINCT g.id)
		FROM games g
		JOIN game_results gr ON gr.game_id = g.id
		WHERE gr.player_id IN (SELECT player_id FROM current_roster)
		  AND gr.position % 2 = 1
		  AND g.machine_key IS NOT NULL
		GROUP BY g.machine_key
	`, teamKey, teamKey)
	if err != nil {
		return nil, fmt.Errorf("query roster picks: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	result := make(map[string]int)
	for rows.Next() {
		var key string
		var n int
		if err := rows.Scan(&key, &n); err != nil {
			return nil, fmt.Errorf("scan roster picks: %w", err)
		}
		result[key] = n
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roster picks: %w", err)
	}

	return result, nil
}

// GetLeagueP50 returns the league-wide P50 score for each machine. League P50
// is computed across all scores by players on any team's current roster.
// Recent seasons count for more.
func (s *SQLiteStore) GetLeagueP50(ctx context.Context) (map[string]float64, error) {
	latest, err := s.MaxSeasonNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("get latest season: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		WITH current_roster_all AS (
			SELECT DISTINCT r.player_id
			FROM rosters r
			JOIN teams t ON t.id = r.team_id
			WHERE t.season_id = (SELECT MAX(season_id) FROM teams)
		)
		SELECT g.machine_key, s.number, gr.score
		FROM game_results gr
		JOIN games g ON g.id = gr.game_id
		JOIN matches m ON m.id = g.match_id
		JOIN seasons s ON s.id = m.season_id
		WHERE gr.player_id IN (SELECT player_id FROM current_roster_all)
		  AND g.machine_key IS NOT NULL
	`)
	if err != nil {
		return nil, fmt.Errorf("query league P50: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	machines := make(map[string][]scoredGame)
	for rows.Next() {
		var machine string
		var g scoredGame
		if err := rows.Scan(&machine, &g.season, &g.score); err != nil {
			return nil, fmt.Errorf("scan league P50: %w", err)
		}
		machines[machine] = append(machines[machine], g)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate league P50: %w", err)
	}

	result := make(map[string]float64, len(machines))
	for machine, games := range machines {
		result[machine] = percentile(games, latest, 0.5)
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
// Recent seasons count for more. If venueKey is non-empty, filters to games
// played at that venue. Results are ordered by play count descending.
func (s *SQLiteStore) GetSinglePlayerMachineStats(ctx context.Context, playerName, venueKey string) ([]PlayerMachineStats, error) {
	latest, err := s.MaxSeasonNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("get latest season: %w", err)
	}

	query := `
		SELECT g.machine_key, s.number, gr.score
		FROM game_results gr
		JOIN players p ON p.id = gr.player_id
		JOIN games g ON g.id = gr.game_id
		JOIN matches m ON m.id = g.match_id
		JOIN seasons s ON s.id = m.season_id
		WHERE p.name = ?
		  AND g.machine_key IS NOT NULL
	`
	args := []any{playerName}

	if venueKey != "" {
		query += " AND m.venue_id = (SELECT id FROM venues WHERE key = ?)"
		args = append(args, venueKey)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query single player machine stats: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	machines := make(map[string][]scoredGame)
	for rows.Next() {
		var machine string
		var g scoredGame
		if err := rows.Scan(&machine, &g.season, &g.score); err != nil {
			return nil, fmt.Errorf("scan single player machine stats: %w", err)
		}
		machines[machine] = append(machines[machine], g)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate single player machine stats: %w", err)
	}

	stats := make([]PlayerMachineStats, 0, len(machines))
	for machine, games := range machines {
		stats = append(stats, PlayerMachineStats{
			MachineKey: machine,
			Games:      len(games),
			P50Score:   percentile(games, latest, 0.5),
			P90Score:   percentile(games, latest, 0.9),
		})
	}
	slices.SortFunc(stats, func(a, b PlayerMachineStats) int {
		return cmp.Or(cmp.Compare(b.Games, a.Games), cmp.Compare(a.MachineKey, b.MachineKey))
	})
	return stats, nil
}

// GetPlayer returns a player's summary info (latest season).
func (s *SQLiteStore) GetPlayer(ctx context.Context, playerName string) (PlayerSummary, error) {
	var p PlayerSummary
	err := s.db.QueryRowContext(ctx, `
		SELECT p.name, t.key, t.name, COALESCE(ipr.ipr, 0)
		FROM players p
		JOIN rosters r ON r.player_id = p.id
		JOIN teams t ON t.id = r.team_id
		LEFT JOIN player_iprs ipr ON ipr.name = p.name
		WHERE p.name = ?
		ORDER BY t.season_id DESC
		LIMIT 1
	`, playerName).Scan(&p.Name, &p.TeamKey, &p.Team, &p.IPR)
	if err != nil {
		return p, fmt.Errorf("get player: %w", err)
	}
	return p, nil
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
// the team (latest season with that team key). Recent seasons count for more.
// If venueKey is non-empty, filters to games played at that venue.
// Results are ordered by P50 score descending.
func (s *SQLiteStore) GetPlayerMachineStats(ctx context.Context, teamKey, machineKey, venueKey string) ([]PlayerStats, error) {
	latest, err := s.MaxSeasonNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("get latest season: %w", err)
	}

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
		)
		SELECT p.name, COALESCE(ipr.ipr, 0), s.number, gr.score
		FROM game_results gr
		JOIN players p ON p.id = gr.player_id
		LEFT JOIN player_iprs ipr ON ipr.name = p.name
		JOIN games g ON g.id = gr.game_id
		JOIN matches m ON m.id = g.match_id
		JOIN seasons s ON s.id = m.season_id
		WHERE g.machine_key = ?
		  AND p.id IN (SELECT player_id FROM current_roster)
	`
	args := []any{teamKey, teamKey, machineKey}

	if venueKey != "" {
		query += " AND m.venue_id = (SELECT id FROM venues WHERE key = ?)"
		args = append(args, venueKey)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query player stats: %w", err)
	}
	defer rows.Close() //nolint:errcheck // Read-only query.

	iprs := make(map[string]int)
	players := make(map[string][]scoredGame)
	for rows.Next() {
		var name string
		var ipr int
		var g scoredGame
		if err := rows.Scan(&name, &ipr, &g.season, &g.score); err != nil {
			return nil, fmt.Errorf("scan player stats: %w", err)
		}
		iprs[name] = ipr
		players[name] = append(players[name], g)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate player stats: %w", err)
	}

	stats := make([]PlayerStats, 0, len(players))
	for name, games := range players {
		stats = append(stats, PlayerStats{
			Name:     name,
			Games:    len(games),
			P50Score: percentile(games, latest, 0.5),
			P90Score: percentile(games, latest, 0.9),
			IPR:      iprs[name],
		})
	}
	slices.SortFunc(stats, func(a, b PlayerStats) int {
		return cmp.Or(cmp.Compare(b.P50Score, a.P50Score), cmp.Compare(a.Name, b.Name))
	})
	return stats, nil
}
