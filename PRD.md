# MNP Strategic Commands PRD

## Problem

The current `mnp query` command requires SQL knowledge and understanding of
schema quirks (e.g., points stored as 2x). This makes it hard for users (and
LLMs) to answer common strategic questions reliably.

Captains preparing for matches need answers to predictable questions:

- What machines should we pick for our rounds?
- What will the opponent likely pick, and how should we respond?
- Who should play each machine?

Pre-baked commands can answer these reliably, hiding schema complexity and
surfacing the right metrics (average score, not win rate).

## Match Flow Context

Each match has 4 rounds. The **picking team** selects machines AND their
players; the responding team only chooses who to send.

| Round | Type | Picking Team | Responding Team |
|-------|------|--------------|-----------------|
| R1 | Doubles (4 games) | Away | Home |
| R2 | Singles (7 games) | Home | Away |
| R3 | Singles (7 games) | Away | Home |
| R4 | Doubles (4 games) | Home | Away |

All picks for a round are made before games start, then all games run
simultaneously. This tool is for **pre-match planning** - generating a game
plan before match night.

## Design Principles

1. **Score-based, not win-rate-based**: Average score is more predictive than
   win rate, which is polluted by opponent strength.

2. **Venue-specific with global fallback**: Venue data is most relevant but
   often sparse. Show both when useful.

3. **Opponent context optional but valuable**: Recommendations improve when
   you know who you're playing against.

4. **Planning, not live support**: Commands generate game plans for the week
   before, not real-time decisions at the venue.

## Commands

| Command | Purpose |
|---------|---------|
| `plan` | **Primary** - Generate full match game plan |
| `scout` | Team strengths/weaknesses (general or at venue) |
| `matchup` | Head-to-head team comparison at venue |
| `recommend` | Drill-down: who should play a specific machine |

> **Note**: Examples below use markdown tables for readability. Actual CLI output
> will use bordered ASCII tables for terminal display.

---

### `mnp plan`

Generate a full match game plan. Primary command for pre-match preparation.

```
mnp plan <venue> <our-team> <their-team> --home|--away
```

**Example**:

```
mnp plan ANC CRA PYC --away
```

**Output**:

```
=== CRA vs PYC at ANC (CRA away) ===

Machines at ANC: TZ, MM, AFM, LOTR, GB, TSPP, IMDN, BSD

TEAM IPR: CRA 32 (handicap 9) vs PYC 38 (handicap 6)

YOUR PICKING ROUNDS:

  R1 Doubles (away picks):
    Recommended picks:
      1. TZ    → Alice (IPR 4, avg 52M) + Bob (IPR 3, avg 38M)
                 vs PYC best: Eve (avg 25M) + Frank (avg 16M)
                 Edge: +49M combined, likely 5-0
      2. MM    → Carol (IPR 2, avg 35M) + Dave (IPR 3, avg 32M)
                 vs PYC best: Gina (avg 18M) + Hank (avg 12M)
                 Edge: +37M combined, likely 5-0
      3. AFM   → Eve (IPR 2, avg 28M) + Frank (IPR 2, avg 25M)
                 vs PYC best: Iris (avg 15M) + Jack (avg 9M)
                 Edge: +29M combined, likely 5-0
      4. LOTR  → Gina (IPR 1, avg 18M) + Hank (IPR 1, avg 15M)
                 vs PYC best: Kate (avg 22M) + Leo (avg 18M)
                 Edge: -7M combined, likely 1-4

  R3 Singles (away picks):
    Recommended picks:
      1. TZ    → Alice (avg 52M) vs PYC best Eve (avg 38M), Edge +14M
      2. GB    → Bob (avg 45M) vs PYC best Frank (avg 35M), Edge +10M
      ...

THEIR LIKELY PICKS (prepare responses):

  R2 Singles (home picks):
    PYC likely picks & your best responses:
      TZ   → they send Eve (avg 38M)    → respond Carol (avg 35M), -3M
      MM   → they send Frank (avg 40M)  → respond Alice (avg 48M), +8M
      AFM  → they send Gina (avg 32M)   → respond Bob (avg 30M), -2M
      ...

  R4 Doubles (home picks):
    PYC likely picks & your best responses:
      TZ   → Eve (avg 25M) + Frank (avg 16M) → respond Alice (52M) + Bob (38M), +49M
      ...

SUMMARY:
  - Strong picks: TZ, MM, GB (you have clear edge)
  - Avoid: LOTR, BSD (they're stronger)
  - Key players: Alice (your best on 5 machines), Eve (their best on 4)
  - Watch out: If they pick TSPP, you have no good counter
```

Use `scout`, `matchup`, and `recommend` for drilling into specific "what if"
scenarios.

---

### `mnp scout`

Scout a team's strengths and weaknesses.

```
mnp scout <team> [--venue <venue>]
```

#### Without venue: Global team profile

```
mnp scout PYC
```

Shows team's performance across all machines they've played (any venue):

| Machine | Games | Avg | Max | Top Players |
|---------|-------|-----------|-----------|-------------|
| TZ      | 45    | 52M       | 180M      | Dave (58M), Eve (45M) |
| MM      | 38    | 28M       | 95M       | Frank (32M), Dave (25M) |
| AFM     | 22    | 15M       | 40M       | Eve (18M), Gina (14M) |

**Analysis**:
- **Strongest machines**: Highest avg scores, most experience
- **Weakest machines**: Lowest avg scores
- **Most experienced**: Highest game counts

Useful for general opponent scouting before checking venue specifics.

#### With venue: Venue-specific scouting

```
mnp scout PYC --venue ANC
```

Shows team's performance on machines at that specific venue:

| Machine | Games | Avg | Max | Top Players |
|---------|-------|-----------|-----------|-------------|
| TZ      | 12    | 45M       | 120M      | Alice (48M), Bob (35M) |
| MM      | 8     | 22M       | 55M       | Carol (25M), Alice (20M) |
| AFM     | 0     | -         | -         | (no data - see global) |

**Analysis**:
- **Likely picks**: High avg score + sufficient experience at venue
- **Likely avoids**: Low avg score or no venue experience
- **Wildcards**: High variance (large max/avg spread)

For machines with no venue-specific data, show global stats as fallback with
indicator that it's not venue-specific.

---

### `mnp matchup`

Compare two teams head-to-head at a venue.

```
mnp matchup <venue> <team1> <team2>
```

Venue is required - it determines what machines are in play.

**Example**:

```
mnp matchup ANC CRA PYC
```

**Output**: Side-by-side comparison per machine at venue:

| Machine | CRA Avg | CRA Max | PYC Avg | PYC Max | Edge |
|---------|---------|---------|---------|---------|------|
| TZ      | 45M     | 120M    | 38M     | 95M     | CRA +7M |
| MM      | 22M     | 55M     | 35M     | 80M     | PYC +13M |
| AFM     | 30M     | 60M     | 28M     | 50M     | Even |

**Analysis**:
- **CRA advantages**: Machines where CRA avg >> PYC avg
- **PYC advantages**: Machines where PYC avg >> CRA avg
- **Contested**: Both teams strong, expect close games
- **Key singles matchups**: Historical head-to-head between likely players

---

### `mnp recommend`

Recommend which player(s) should play a specific machine.

```
mnp recommend <team> <machine> [--venue <venue>] [--vs <opponent>]
```

**Output** (basic):

```
mnp recommend CRA TZ
```

| Player | Games | Avg | Max |
|--------|-------|-----|-----|
| Alice  | 15    | 48M | 120M |
| Bob    | 8     | 35M | 80M |
| Carol  | 4     | 28M | 45M |

**Output** (with `--venue ANC`):

```
mnp recommend CRA TZ --venue ANC
```

At ANC:

| Player | Games | Avg | Max |
|--------|-------|-----|-----|
| Alice  | 3     | 52M | 95M |
| Bob    | 1     | 40M | 40M |

Global (for context):

| Player | Games | Avg | Max |
|--------|-------|-----|-----|
| Alice  | 15    | 48M | 120M |
| Bob    | 8     | 35M | 80M |
| Carol* | 4     | 28M | 45M |

*No ANC data

**Output** (with `--vs PYC`):

```
mnp recommend CRA TZ --vs PYC
```

CRA options:

| Player | Games | Avg | Max |
|--------|-------|-----|-----|
| Alice  | 15    | 48M | 120M |
| Bob    | 8     | 35M | 80M |

PYC likely players:

| Player | Games | Avg | Max |
|--------|-------|-----|-----|
| Dave   | 12    | 42M | 95M |
| Eve    | 6     | 38M | 70M |

Assessment: Alice outscores PYC's best (Dave) by ~6M avg. Strong pick.

## Metrics

**Primary metric**: Average score per machine (not win rate)

- More predictive of future performance
- Independent of historical opponent strength
- Directly comparable across players/teams

**Secondary metrics**:
- Max score (indicates ceiling/potential)
- Game count (sample size / confidence)
- Score variance (consistency indicator)

## Data Requirements

All data exists in current schema. Key queries:

```sql
-- Player avg/max score on machine
SELECT 
    p.name,
    COUNT(*) as games,
    AVG(gr.score) as avg_score,
    MAX(gr.score) as max_score
FROM game_results gr
JOIN players p ON p.id = gr.player_id
JOIN games g ON g.id = gr.game_id
WHERE g.machine_key = ?
  AND gr.team_id IN (SELECT id FROM teams WHERE key = ?)
GROUP BY p.id
ORDER BY avg_score DESC;

-- Venue-specific: add JOIN matches + WHERE venue_id = ?
```

## Out of Scope (Future)

1. **Recent form weighting**: Weight recent games higher than old games
2. **Doubles pair recommendations**: Who partners well together
3. **Round-by-round strategy**: Different picks for R1 vs R4
4. **Opponent tendency prediction**: ML-based pick prediction
5. **Live match tracking**: Real-time score updates and adjusted predictions

## Open Questions

1. **Sample size thresholds**: How many games before data is "reliable"?
   Suggest: Show all data but flag low sample sizes (< 3 games).

2. **Score normalization**: Scores vary wildly by machine. Is raw average
   meaningful for cross-machine comparison? Probably fine for same-machine
   player comparison (primary use case).

3. **Season scoping**: Should stats be all-time or current season?
   Suggest: Default to all-time, flag for `--season N` to filter.

4. **Team key vs name**: Commands use team key (CRA) for brevity. Should also
   support full names? Probably yes, with fuzzy matching.
