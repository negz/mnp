---
name: mnp
description: Query Monday Night Pinball league data. Use when analyzing MNP matches, players, teams, machines, or strategic questions about upcoming games.
---

# Monday Night Pinball Data

Query Seattle's Monday Night Pinball league data using SQLite.

## League & Match Rules

**Full rules**: [Match Rules](https://www.mondaynightpinball.com/matchrules) | [League Rules](https://www.mondaynightpinball.com/leaguerules)

### Match Structure

Each match has 4 rounds:

| Round | Type | Games | Points/Game | Max Points |
|-------|------|-------|-------------|------------|
| 1 | Doubles | 4 | 5 | 20 |
| 2 | Singles | 7 | 3 | 21 |
| 3 | Singles | 7 | 3 | 21 |
| 4 | Doubles | 4 | 5 | 20 |

- **Doubles scoring**: 1 point per opponent beaten + 1 bonus for highest combined team score (allows half-points like 4-1, 3-2)
- **Singles scoring**: 2-1 win normally, 3-0 if you double opponent's score
- Players can only play once per round

### Total Match Scoring

| Category | Max | How |
|----------|-----|-----|
| Gameplay | 82 | Sum of all game points |
| Participation bonus | 9 | Full roster (10 players) each playing 3+ games |
| Handicap | 15 | 1 point per 2 IPR below 50 |
| **Total** | **106** | |

### IPR (Individual Player Rating)

IPR rates players 1-6 based on IFPA ranking and Matchplay.events rating:
- 1 = novice (bottom 30%)
- 6 = elite (top 5%)

**Team IPR** = sum of lineup players' IPRs

**Handicap formula**: `floor((50 - team_IPR) / 2)`, max 15 points

Example: Team IPR of 19 → (50-19)/2 = 15.5 → **15 handicap points**

### Strategic Implications

- Lower-IPR teams get significant handicap advantage
- A full 10-player roster with average IPR 3.6 = team IPR 36, handicap 7
- Short-handed teams (fewer players) have lower IPR but also lose participation bonus
- Machine selection matters: pick machines your players excel at, opponents struggle with

## Querying

```bash
cd ~/control/negz/mnp && ./mnp query "SELECT ..."
```

Data syncs automatically on first use.

### View Schema

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

## Notes

- Points in the database are stored as **2x actual values** to handle half-points as integers (e.g., 3 points = 6, 2.5 points = 5)
- Use `--force` flag to force a full data re-sync if needed
