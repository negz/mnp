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
surfacing the right metrics (P50/P90 scores, not win rate).

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

1. **Score-based, not win-rate-based**: P50/P90 scores are more predictive than
   win rate, which is polluted by opponent strength.

2. **Relative strength for cross-machine comparison**: Raw scores can't be
   compared across machines (2B on AFM is average; 500M on GOT is elite).
   Scores are normalized as % above/below league-wide P50 per machine.

3. **Venue-specific with global fallback**: Venue data is most relevant but
   often sparse. Show both when useful.

4. **Opponent context optional but valuable**: Recommendations improve when
   you know who you're playing against.

5. **Planning, not live support**: Commands generate game plans for the week
   before, not real-time decisions at the venue.

## Commands

| Command | Purpose |
|---------|---------|
| `plan` | **Primary** - Generate full match game plan |
| `scout` | Team strengths/weaknesses (general or at venue) |
| `matchup` | Head-to-head team comparison at venue |
| `recommend` | Drill-down: who should play a specific machine |
| `venues` | List all venues with keys |
| `machines` | List all machines with keys |

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
    Recommended picks (sorted by your edge over opponent):
      1. TZ    → Alice (IPR 4, P50 42M) + Bob (IPR 3, P50 30M)
                 vs PYC best: Eve (P50 20M) + Frank (P50 12M)
                 You +30% vs avg, them +10% → Edge CRA +20%
      2. MM    → Carol (IPR 2, P50 28M) + Dave (IPR 3, P50 25M)
                 vs PYC best: Gina (P50 14M) + Hank (P50 10M)
                 You +40% vs avg, them -15% → Edge CRA +55%
      3. AFM   → Eve (IPR 2, P50 22M) + Frank (IPR 2, P50 20M)
                 vs PYC best: Iris (P50 12M) + Jack (P50 8M)
                 You +15% vs avg, them -12% → Edge CRA +27%
      4. LOTR  → Gina (IPR 1, P50 14M) + Hank (IPR 1, P50 12M)
                 vs PYC best: Kate (P50 18M) + Leo (P50 15M)
                 You -10% vs avg, them +5% → Edge PYC +15%

  R3 Singles (away picks):
    Recommended picks (sorted by your edge over opponent):
      1. TZ    → Alice (P50 42M, +30%) vs PYC best Eve (P50 28M, +10%), Edge CRA +20%
      2. GB    → Bob (P50 35M, +25%) vs PYC best Frank (P50 28M, +5%), Edge CRA +20%
      ...

THEIR LIKELY PICKS (prepare responses):
  Predicted by opponent's relative strength (sorted strongest first):

  R2 Singles (home picks):
    PYC likely picks & your best responses:
      MM   → they send Frank (P50 32M, +40%)  → respond Alice (P50 38M, +45%), Edge CRA +5%
      TZ   → they send Eve (P50 28M, +10%)    → respond Carol (P50 26M, +5%), Edge PYC +5%
      AFM  → they send Gina (P50 25M, +8%)    → respond Bob (P50 24M, +5%), Edge PYC +3%
      ...

  R4 Doubles (home picks):
    PYC likely picks & your best responses:
      MM   → Frank + Gina (+40%) → respond Carol + Dave (+35%), Edge PYC +5%
      ...

SUMMARY:
  - Strong picks: TZ, GB (your relative strength >> theirs)
  - Avoid: LOTR, BSD (they're stronger relative to league avg)
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

Shows current roster's performance across all machines they've played (any
venue). Sorted by play count (games by current roster players only).

| Machine | Games | P50 | P90 | Likely Players |
|---------|-------|-----|-----|----------------|
| TZ      | 45    | 35M (+30%) | 68M | Dave (42M), Eve (38M) |
| MM      | 38    | 18M (+40%) | 35M | Frank (22M), Dave (20M) |
| AFM     | 22    | 10M (-12%) | 22M | Eve (12M), Gina (11M) |

The `(+30%)` on P50 shows how the team's P50 compares to the league-wide
P50 for that machine. This makes strength comparable across machines with
very different scoring scales. Likely Players shows the two players with the
most games on that machine (minimum 3 games), with their P50 in parentheses.

**Analysis**:
- **Strongest machines**: High relative strength (large positive %)
- **Weakest machines**: Low relative strength (negative %) or no data
- Games and scores only count plays by players currently on the team's roster

Useful for general opponent scouting before checking venue specifics.

#### With venue: Venue-specific scouting

```
mnp scout PYC --venue ANC
```

Shows current roster's performance on machines at that specific venue.
Sorted by play count (games by current roster players only).

| Machine | Games | P50 | P90 | Likely Players |
|---------|-------|-----|-----|----------------|
| TZ      | 12    | 30M (+13%) | 55M | Alice (35M), Bob (32M) |
| MM      | 8     | 15M (+10%) | 30M | Carol (18M), Alice (16M) |
| AFM     | 0     | -          | -   | (no data - see global) |

**Analysis**:
- **Likely picks**: High play count + sufficient experience at venue
- **Likely avoids**: Low play count or no venue experience
- **Wildcards**: High variance (large P90/P50 spread)

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

**Output**: Side-by-side comparison per machine at venue, sorted by edge
(descending, your best machines first):

| Machine | CRA P50 | CRA Likely | PYC P50 | PYC Likely | Edge |
|---------|---------|------------|---------|------------|------|
| TZ      | 35M     | 40M        | 28M     | 28M        | CRA +12M |
| AFM     | 22M     | 25M        | 20M     | 20M        | CRA +5M |
| MM      | 18M     | 21M        | 25M     | 30M        | PYC +9M |

- **P50**: Team-wide median (whole roster's performance on this machine)
- **Likely**: Average P50 of the two players with the most games on this
  machine (minimum 3 games each). Represents what the team will actually
  score when they send their best available players.
- **Edge**: Difference between the two Likely columns. Since this is a
  same-machine comparison, raw score deltas are valid.

The gap between P50 and Likely is informative — a large gap means the team
has specialists on that machine. A small gap means their roster is evenly
spread.

**Analysis**:
- **CRA advantages**: Machines where CRA Likely >> PYC Likely (pick these)
- **PYC advantages**: Machines where PYC Likely >> CRA Likely (avoid these)
- **Contested**: Small edge — expect close games

Use `scout` for more detail on who the likely players are and their
individual stats.

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

| Player | Games | P50 | P90 |
|--------|-------|-----|-----|
| Alice  | 15    | 32M (+37%) | 65M |
| Bob    | 8     | 22M (avg)  | 42M |
| Carol  | 4     | 18M (-20%) | 30M |

Sorted by P50 score descending. The `(+37%)` shows how each player's P50
compares to the league-wide P50 for this machine.

**Output** (with `--venue ANC`):

```
mnp recommend CRA TZ --venue ANC
```

At ANC:

| Player | Games | P50 | P90 |
|--------|-------|-----|-----|
| Alice  | 3     | 35M (+49%) | 70M |
| Bob    | 1     | 40M (+14%) | 40M |

Global (for context):

| Player | Games | P50 | P90 |
|--------|-------|-----|-----|
| Alice  | 15    | 32M (+37%) | 65M |
| Bob    | 8     | 22M (avg)  | 42M |
| Carol* | 4     | 18M (-20%) | 30M |

*No ANC data

**Output** (with `--vs PYC`):

```
mnp recommend CRA TZ --vs PYC
```

CRA options:

| Player | Games | P50 | P90 |
|--------|-------|-----|-----|
| Alice  | 15    | 32M (+37%) | 65M |
| Bob    | 8     | 22M (avg)  | 42M |

PYC likely players:

| Player | Games | P50 | P90 |
|--------|-------|-----|-----|
| Dave   | 12    | 28M (+20%) | 55M |
| Eve    | 6     | 25M (+9%)  | 48M |

Assessment: Alice outscores PYC's best (Dave) by ~4M P50. Strong pick.

---

### `mnp venues`

List all venues with their keys.

```
mnp venues
```

| Key | Name |
|-----|------|
| ANC | Add-a-Ball |
| SAM | Sam's Tavern |
| FUL | Full Tilt Ballard |
| ... | ... |

---

### `mnp machines`

List all machines with their keys.

```
mnp machines
```

| Key | Name | Year | Manufacturer |
|-----|------|------|--------------|
| TZ | Twilight Zone | 1993 | Bally |
| MM | Medieval Madness | 1997 | Williams |
| AFM | Attack from Mars | 1995 | Bally |
| ... | ... | ... | ... |

Useful for looking up short codes when using other commands.

## Metrics

**Primary metric**: P50 (median) score per machine

- **P50**: What this player will probably score — the most likely outcome
- More predictive than win rate, which is polluted by opponent strength
- Directly comparable across players on the **same machine**
- Decorated with `(+30%)` showing % above/below league-wide P50 for that
  machine, enabling cross-machine comparison

**Relative strength**: % above/below league-wide P50 for that machine

- Solves the cross-machine comparison problem: raw scores vary wildly by
  machine (2B on AFM is average; 500M on GOT is elite), so raw P50 can't
  be compared or sorted across machines
- `(+30%)` means the player/team's P50 is 30% above league-wide P50
- League average is computed across all current roster players league-wide
- Used for: sorting `scout` display, `matchup` edge calculation, `plan`
  pick ordering, and decorating P50 in all commands

**Secondary metric**: P90 (90th percentile) score per machine

- What they score on a great day — their realistic ceiling
- More robust than max score, which can be a single lucky fluke
- At small sample sizes (< 10 games) P90 approaches max, which is
  acceptable — with few games there isn't enough data to distinguish
  ceiling from fluke anyway

**How each command uses these metrics**:

| Command | Sorted by | Rationale |
|---------|-----------|-----------|
| `recommend` | P50 score (descending) | Same machine — raw scores are directly comparable |
| `scout` | Play count (descending) | Shows what the team actually plays; relative strength decoration shows quality |
| `matchup` | Edge (your Likely minus their Likely) | Same-machine comparison — shows where your likely players beat theirs |
| `plan` picking rounds | Edge (your relative strength minus theirs) | Pick machines where your advantage is largest |
| `plan` opponent predictions | Opponent's relative strength (descending) | Predicts what they'll pick — their strongest machines first |

**Other displayed metrics**:
- Game count (sample size / confidence)

## Data Requirements

All data exists in current schema. Key queries:

```sql
-- Player P50/P90 score on machine (requires sorting + offset)
SELECT 
    p.name,
    COUNT(*) as games
FROM game_results gr
JOIN players p ON p.id = gr.player_id
JOIN games g ON g.id = gr.game_id
WHERE g.machine_key = ?
  AND gr.team_id IN (SELECT id FROM teams WHERE key = ?)
GROUP BY p.id
ORDER BY games DESC;

-- P50 and P90 calculated per-player via window functions (ROW_NUMBER + offset)
-- Venue-specific: add JOIN matches + WHERE venue_id = ?
-- League average: same query without team filter
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

2. **Season scoping**: Should stats be all-time or current season?
   Suggest: Default to all-time, flag for `--season N` to filter.

3. **Team key vs name**: Commands use team key (CRA) for brevity. Should also
   support full names? Probably yes, with fuzzy matching.
