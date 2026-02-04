---
name: mnp
description: Query Monday Night Pinball league data. Use when analyzing MNP matches, players, teams, machines, or strategic questions about upcoming games.
---

# Monday Night Pinball Data

Query Seattle's Monday Night Pinball league data using SQLite.

## Database Location

```
$XDG_CACHE_HOME/mnp/mnp.db  (default: ~/.cache/mnp/mnp.db)
```

## Querying

Use the mnp binary from the repo:

```bash
cd ~/control/negz/mnp && ./mnp query "SELECT ..."
```

Or use sqlite3 directly:

```bash
sqlite3 -header -column ~/.cache/mnp/mnp.db "SELECT ..."
```

For complex queries, use a heredoc:

```bash
sqlite3 -header -column ~/.cache/mnp/mnp.db <<'SQL'
SELECT ...
FROM ...
SQL
```

## Schema

Run `./mnp schema` to see the documented schema:

```bash
cd ~/control/negz/mnp && ./mnp schema
```

## Common Queries

### Player's win rate on a specific machine

```sql
SELECT 
    p.name,
    m.name as machine,
    COUNT(*) as games,
    SUM(gr.points_won) as points_won,
    SUM(gr.points_possible) as points_possible,
    ROUND(100.0 * SUM(gr.points_won) / SUM(gr.points_possible), 1) as win_pct
FROM game_results gr
JOIN players p ON p.id = gr.player_id
JOIN games g ON g.id = gr.game_id
JOIN machines m ON m.key = g.machine_key
WHERE p.name = 'Player Name'
  AND g.machine_key = 'MM'
GROUP BY p.id, g.machine_key;
```

### Player's overall stats

```sql
SELECT 
    p.name,
    COUNT(DISTINCT gr.game_id) as games,
    SUM(gr.points_won) as points_won,
    SUM(gr.points_possible) as points_possible,
    ROUND(100.0 * SUM(gr.points_won) / SUM(gr.points_possible), 1) as win_pct
FROM game_results gr
JOIN players p ON p.id = gr.player_id
WHERE p.name = 'Player Name'
GROUP BY p.id;
```

### Best players on a machine (min 5 games)

```sql
SELECT 
    p.name,
    COUNT(*) as games,
    SUM(gr.points_won) as points_won,
    SUM(gr.points_possible) as points_possible,
    ROUND(100.0 * SUM(gr.points_won) / SUM(gr.points_possible), 1) as win_pct
FROM game_results gr
JOIN players p ON p.id = gr.player_id
JOIN games g ON g.id = gr.game_id
WHERE g.machine_key = 'TZ'
GROUP BY p.id
HAVING games >= 5
ORDER BY win_pct DESC
LIMIT 20;
```

### Player's home vs away performance

```sql
SELECT 
    p.name,
    CASE WHEN t.home_venue_id = mat.venue_id THEN 'home' ELSE 'away' END as location,
    COUNT(*) as games,
    SUM(gr.points_won) as points_won,
    SUM(gr.points_possible) as points_possible,
    ROUND(100.0 * SUM(gr.points_won) / SUM(gr.points_possible), 1) as win_pct
FROM game_results gr
JOIN players p ON p.id = gr.player_id
JOIN games g ON g.id = gr.game_id
JOIN matches mat ON mat.id = g.match_id
JOIN teams t ON t.id = gr.team_id
WHERE p.name = 'Player Name'
GROUP BY p.id, location;
```

### Team roster with player stats for current season

```sql
SELECT 
    p.name,
    r.role,
    COUNT(DISTINCT gr.game_id) as games,
    ROUND(100.0 * SUM(gr.points_won) / NULLIF(SUM(gr.points_possible), 0), 1) as win_pct
FROM rosters r
JOIN players p ON p.id = r.player_id
JOIN teams t ON t.id = r.team_id
JOIN seasons s ON s.id = t.season_id
LEFT JOIN game_results gr ON gr.player_id = p.id AND gr.team_id = t.id
WHERE t.key = 'CRA' AND s.number = 23
GROUP BY p.id
ORDER BY r.role, win_pct DESC;
```

### Player performance by machine era

```sql
SELECT 
    p.name,
    CASE 
        WHEN m.year < 1978 THEN 'EM'
        WHEN m.year < 1991 THEN 'Early SS'
        WHEN m.year < 2000 THEN 'DMD'
        WHEN m.year < 2013 THEN 'Early Modern'
        ELSE 'LCD'
    END as era,
    COUNT(*) as games,
    ROUND(100.0 * SUM(gr.points_won) / SUM(gr.points_possible), 1) as win_pct
FROM game_results gr
JOIN players p ON p.id = gr.player_id
JOIN games g ON g.id = gr.game_id
JOIN machines m ON m.key = g.machine_key
WHERE p.name = 'Player Name'
  AND m.year IS NOT NULL
GROUP BY p.id, era
ORDER BY era;
```

### Head-to-head: two players' direct matchups

```sql
SELECT 
    p1.name as player1,
    p2.name as player2,
    COUNT(*) as games,
    SUM(CASE WHEN gr1.points_won > gr2.points_won THEN 1 ELSE 0 END) as p1_wins,
    SUM(CASE WHEN gr2.points_won > gr1.points_won THEN 1 ELSE 0 END) as p2_wins
FROM game_results gr1
JOIN game_results gr2 ON gr1.game_id = gr2.game_id AND gr1.player_id != gr2.player_id
JOIN players p1 ON p1.id = gr1.player_id
JOIN players p2 ON p2.id = gr2.player_id
JOIN games g ON g.id = gr1.game_id
WHERE p1.name = 'Player 1'
  AND p2.name = 'Player 2'
  AND g.is_doubles = 0  -- Singles only for true head-to-head
GROUP BY p1.id, p2.id;
```

### Machines at a venue

```sql
SELECT m.key, m.name, m.manufacturer, m.year
FROM venue_machines vm
JOIN machines m ON m.key = vm.machine_key
JOIN venues v ON v.id = vm.venue_id
JOIN seasons s ON s.id = vm.season_id
WHERE v.key = 'ANC' AND s.number = 23
ORDER BY m.year DESC;
```

### Upcoming matches for a team

```sql
SELECT 
    mat.week,
    mat.date,
    CASE WHEN mat.home_team_id = t.id THEN 'vs' ELSE '@' END as side,
    CASE WHEN mat.home_team_id = t.id THEN t2.name ELSE t2.name END as opponent,
    v.name as venue
FROM matches mat
JOIN teams t ON t.id IN (mat.home_team_id, mat.away_team_id)
JOIN teams t2 ON t2.id = CASE WHEN mat.home_team_id = t.id THEN mat.away_team_id ELSE mat.home_team_id END
JOIN venues v ON v.id = mat.venue_id
JOIN seasons s ON s.id = mat.season_id
WHERE t.key = 'CRA' AND s.number = 23
  AND mat.home_points IS NULL  -- Not yet played
ORDER BY mat.week;
```

## Match Rules Summary

- 4 rounds per match: Doubles (R1) → Singles (R2) → Singles (R3) → Doubles (R4)
- Doubles: 5 points per game, 4 games per round
- Singles: 3 points per game, 7 games per round
- Max 82 points per match (+ bonus points for full rosters)
- Players can only play once per round

## Data Sync

Data syncs automatically on first query or when needed:

- **First run**: Full sync (~15-20s) - clones MNP archive, fetches IPDB
- **Subsequent queries**: Incremental (~1s) - git pull + reload current season only
- **IPDB refresh**: Weekly (machine metadata rarely changes)
- **Completed seasons**: Cached permanently (immutable)

### Flags

```bash
# Force full re-sync of all data
./mnp query --force "SELECT ..."

# Show sync progress
./mnp query -v "SELECT ..."
```

### Data Sources

- MNP data archive: match results, rosters, teams, venues
- IPDB database: machine metadata (year, manufacturer, type)
