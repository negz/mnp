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
   compared across machines (2B on AFM is decent; 2B on GOT is great).
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
| `scout` | Team strengths/weaknesses (general or at venue) |
| `matchup` | Head-to-head team comparison at venue |
| `recommend` | Drill-down: who should play a specific machine |
| `teams` | List all teams with home venues |
| `venues` | List all venues with keys |
| `machines` | List all machines with keys |
| `serve` | Start the web UI |

> **Note**: Examples below use markdown tables for readability. Actual CLI output
> uses bordered ASCII tables.

---

### `mnp scout`

Scout a team's strengths and weaknesses.

```
mnp scout <team> [--venue <venue>]
```

#### Without venue: Global team profile

```
mnp scout CRA
```

Shows current roster's performance across all machines they've played (any
venue). Sorted by play count (games by current roster players only).

| Machine | Games | P50 (vs Avg) | P90 | Likely Players |
|---------|-------|--------------|-----|----------------|
| Godzilla | 135 | 192.9M (+178%) | 1.1B | Jay O (131.9M), Connor V (562.6M) |
| Eight Ball | 91 | 280.2K (+106%) | 536.9K | Joshua F (378.3K), Connor V (244.8K) |
| Game of Thrones | 90 | 672.5M (+169%) | 2.1B | Joshua F (1.4B), Nate T (149.6M) |

Footer shows three strongest and three weakest machines by relative strength:

```
Strongest: Toy Story, Xenon, Venom Left
Weakest:   Sinbad, Metallica Remastered, Total Nuclear Annihilation
```

The `(+178%)` in the P50 column shows how the team's P50 compares to the
league-wide P50 for that machine. This makes strength comparable across
machines with very different scoring scales. Likely Players shows the two
players with the most games on that machine (minimum 3 games), with their
P50 in parentheses.

**Analysis**:
- **Strongest machines**: High relative strength (large positive %)
- **Weakest machines**: Low relative strength (negative %) or no data
- Games and scores only count plays by players currently on the team's roster

Useful for general opponent scouting before checking venue specifics.

#### With venue: Venue-specific scouting

```
mnp scout CRA --venue AAB
```

Shows two tables. First, venue-specific stats (only games played at that
venue), then global stats filtered to machines at the venue for context.
Both sorted by play count.

```
At AAB:
```

| Machine | Games | P50 (vs Avg) | P90 | Likely Players |
|---------|-------|--------------|-----|----------------|
| Eight Ball Deluxe | 19 | 953.4K (+45%) | 1.9M | Ken D (898.5K), Chris T (1.1M) |
| Nitro Groundshaker | 13 | 410.8K (+146%) | 650.5K | Joshua F (410.8K), Armand G (108.7K) |
| The Addams Family | 11 | 61.7M (+61%) | 183.4M | Connor V (61.7M), Joshua F (44.2M) |

```
Global (for context):
```

| Machine | Games | P50 (vs Avg) | P90 | Likely Players |
|---------|-------|--------------|-----|----------------|
| Godzilla | 135 | 192.9M (+178%) | 1.1B | Jay O (131.9M), Connor V (562.6M) |
| Monster Bash | 80 | 19.9M (+19%) | 69.8M | Jay O (19.9M), MJ M (17.6M) |
| John Wick* | 20 | 75.0M (+266%) | 145.0M | Armand G (86.6M), Connor V (120.2M) |

Machines with no venue data are marked with `*` in the global table.

The footer shows strongest/weakest by relative strength at the venue:

```
Strongest: Black Knight Sword of Rage, Batman 66, Fathom
Weakest:   Foo Fighters, Mata Hari, Medieval Madness
```

**Analysis**:
- **Likely picks**: High play count + sufficient experience at venue
- **Likely avoids**: Low play count or no venue experience
- **Wildcards**: High variance (large P90/P50 spread)

---

### `mnp matchup`

Compare two teams head-to-head at a venue.

```
mnp matchup <venue> <team-1> <team-2>
```

Venue is required — it determines what machines are in play.

**Example**:

```
mnp matchup AAB CRA PYC
```

**Output**: Side-by-side comparison per machine at venue, sorted by edge
(descending, first team's best machines first):

| Machine | CRA P50 | CRA Likely | PYC P50 | PYC Likely | Edge |
|---------|---------|------------|---------|------------|------|
| Fathom | 446.0K | 2.3M | 258.4K | 258.4K | CRA 781% ▲ |
| John Wick | 75.0M | 103.4M | 9.2M | 15.7M | CRA 559% △ |
| Godzilla | 192.9M | 347.3M | 62.2M | 61.2M | CRA 467% ▲ |
| The Addams Family | 69.1M | 69.0M | 37.7M | 91.4M | PYC 32% △ |
| Foo Fighters | 76.2M | 52.0M | 126.5M | 114.0M | PYC 119% ▼ |

```
▲ high confidence  △ medium  ▼ low (based on likely players' games)
CRA advantages: Fathom, John Wick, Godzilla, Uncanny X-men, ...
PYC advantages: The Addams Family, Foo Fighters
```

- **P50**: Team-wide median (whole roster's performance on this machine)
- **Likely**: Average P50 of the two players with the most games on this
  machine (no minimum game threshold — see confidence indicator). Represents
  what the team will likely score when they send their most likely players.
- **Edge**: Percentage difference between the two Likely columns, decorated
  with a confidence indicator.

Machines where one team has data but the other doesn't show just the team
name in the Edge column (e.g. `CRA`) with no percentage.

**Confidence indicator**: Each edge is decorated with ▲/△/▼ based on how
much data backs the Likely values being compared. Confidence is determined
by the average game count of each team's top 2 players on that machine,
taking the minimum across both teams:

- **▲** (high): Both teams' likely players average 10+ games
- **△** (medium): Both teams' likely players average 3–9 games
- **▼** (low): Either team's likely players average fewer than 3 games

The gap between P50 and Likely is informative — a large gap means the team
has specialists on that machine. A small gap means their roster is evenly
spread.

Note: `scout` applies a 3-game minimum for Likely Players, but `matchup`
does not. In a matchup you may be forced to respond to your opponent's
picks, so you want to see the best a team has even if based on a small
sample. The confidence indicator flags when this is the case.

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
mnp recommend CRA Godzilla
```

| Player | Games | P50 (vs Avg) | P90 |
|--------|-------|--------------|-----|
| Craig R Jones | 1 | 1.2B (+1586%) | 1.2B |
| Connor Vermeys | 55 | 562.6M (+710%) | 1.6B |
| Jay Ostby | 67 | 131.9M (+90%) | 578.0M |
| Justin Ari | 6 | 105.7M (+52%) | 253.5M |

Sorted by P50 score descending.

**Output** (with `--venue AAB`):

```
mnp recommend CRA Godzilla --venue AAB
```

Shows venue-specific table first, then global with `*` marking players who
have no data at that venue:

```
At AAB:
```

| Player | Games | P50 (vs Avg) | P90 |
|--------|-------|--------------|-----|
| Justin Ari | 1 | 253.5M (+265%) | 253.5M |
| Chris Tinney | 1 | 151.0M (+117%) | 151.0M |
| Jay Ostby | 3 | 103.5M (+49%) | 163.1M |

```
Global (for context):
```

| Player | Games | P50 (vs Avg) | P90 |
|--------|-------|--------------|-----|
| Craig R Jones* | 1 | 1.2B (+1586%) | 1.2B |
| Connor Vermeys* | 55 | 562.6M (+710%) | 1.6B |
| Jay Ostby | 67 | 131.9M (+90%) | 578.0M |

```
*No AAB data
```

**Output** (with `--vs PYC`):

```
mnp recommend CRA Godzilla --vs PYC
```

```
CRA options:
```

| Player | Games | P50 (vs Avg) | P90 |
|--------|-------|--------------|-----|
| Craig R Jones | 1 | 1.2B (+1586%) | 1.2B |
| Connor Vermeys | 55 | 562.6M (+710%) | 1.6B |

```
PYC likely players:
```

| Player | Games | P50 (vs Avg) | P90 |
|--------|-------|--------------|-----|
| Mason Dunbar | 1 | 220.8M (+218%) | 220.8M |
| Chase Engdall | 3 | 191.7M (+176%) | 360.0M |

```
Assessment: Craig R Jones outscores PYC's best (Mason Dunbar) by ~950.6M P50. Strong pick.
```

When combined with `--venue`, both teams' tables are scoped to that venue.

---

### `mnp venues`

List all venues. Supports optional search term.

```
mnp venues [<search>]
```

| Key | Name |
|-----|------|
| AAB | Add-a-Ball |
| ANC | Another Castle |
| FTB | Full Tilt Ballard |
| ... | ... |

```
mnp venues add
```

| Key | Name |
|-----|------|
| AAB | Add-a-Ball |

---

### `mnp machines`

List all machines. Supports optional search term.

```
mnp machines [<search>]
```

| Key | Name |
|-----|------|
| AFM | Attack From Mars |
| MM | Medieval Madness |
| TZ | Twilight Zone |
| ... | ... |

```
mnp machines twilight
```

| Key | Name |
|-----|------|
| TZ | Twilight Zone |

Useful for looking up short codes when using other commands.

### `mnp teams`

List all teams in the current season. Supports optional search term.

```
mnp teams [<search>]
```

| Key | Name | Venue |
|-----|------|-------|
| CRA | Castle Crashers | Another Castle (ANC) |
| FBZ | Flipper Blitz | Full Tilt Ballard (FTB) |
| GPA | Graveyard Players Association | Coin-Op Game Room (STN) |
| ... | ... | ... |

```
mnp teams castle
```

| Key | Name | Venue |
|-----|------|-------|
| CRA | Castle Crashers | Another Castle (ANC) |

Search matches against both key and name. Useful for finding team keys and
their home venues when using other commands.

---

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
- Used for: sorting `scout` display, `matchup` edge calculation, and
  decorating P50 in all commands

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
| `matchup` | Edge % (first team's Likely vs second) | Same-machine comparison — shows where your likely players beat theirs |

**Other displayed metrics**:
- Game count (sample size / confidence in `scout` and `recommend`)
- Confidence indicator (▲/△/▼ on `matchup` edge, based on likely players'
  game counts)

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

## Web UI (`mnp serve`)

The web UI makes MNP accessible to non-technical team members and usable
during matches on a phone. It mirrors the CLI's analytical commands but
adds a team-focused landing page built around the season schedule.

### Running

```
mnp serve [--addr :8080] [--db mnp.db]
```

Starts an HTTP server. Logs to stdout. Designed to sit behind a reverse
proxy (e.g. Caddy) that terminates TLS. Each replica syncs its own database
on startup and every 24 hours.

### Architecture

- **Single binary**: No separate frontend deployment. One binary to build
  and run.
- **Responsive**: Works on both mobile and desktop browsers.
- **URL-driven state**: All inputs are URL parameters, so pages are
  shareable and bookmarkable (e.g. `/matchup?venue=AAB&t1=CRA&t2=PYC`).
- **Auto-sync**: Syncs data automatically every 24 hours. The MNP data
  archive is updated every few days, so this is sufficient.

### Pages

```
[Home]  [Matchup]  [Scout]  [Recommend]  [Lookup]
```

#### Home (Team Schedule)

The landing page. A team selector (dropdown, remembered in localStorage)
shows that team's upcoming matches for the current season.

```
┌──────────────────────────────────────────┐
│  [Castle Crashers ▾]  Matchup Scout ...  │
├──────────────────────────────────────────┤
│                                          │
│  Wk 3 · Mon Feb 16 · at Add-a-Ball      │
│  vs Pin Pals (PNP)                       │
│  [Matchup]  [Scout PNP]                  │
│                                          │
│  Wk 4 · Mon Feb 23 · at Coin-Op         │
│  @ Tilt Collective (TLC)                 │
│  [Matchup]  [Scout TLC]                  │
│                                          │
│  Wk 5 · Mon Mar 2 · at Another Castle   │
│  vs Silverballers (SLV)                  │
│  [Matchup]  [Scout SLV]                  │
│                                          │
└──────────────────────────────────────────┘
```

- **Future matches only** — past results aren't shown (the MNP website
  covers this well already).
- Each match shows week, date, venue, and opponent.
- **[Matchup]** links to the matchup page with venue and opponent
  pre-filled from the schedule.
- **[Scout]** links to scout with the opponent pre-filled.
- Home/away indicated by "vs" (home) and "@" (away).

#### Matchup

Same as CLI `matchup`. Inputs: venue (dropdown), team 1, team 2 — all
pre-filled when arriving from Home. Shows the comparison table with
confidence indicators and advantage summary. Tapping a machine row could
expand or link to Recommend for that team + machine.

Also accessible standalone from the nav for ad-hoc comparisons (e.g.
playoff matchups not in the regular season schedule).

#### Scout

Same as CLI `scout`. Inputs: team (dropdown), venue (optional dropdown).
Shows venue-specific + global tables when venue is specified. Tapping a
machine row could link to Recommend for that team + machine.

#### Recommend

Same as CLI `recommend`. Inputs: team, machine, optionally venue and
opponent — all dropdowns. Typically reached from Matchup or Scout rather
than directly.

#### Lookup

Collapses the `teams`, `venues`, and `machines` CLI commands into one page
with tabs. Each tab shows a searchable, sortable table. Tapping a team
links to its Scout page.

### Data Requirements

The web UI requires schedule data that the CLI commands don't use. This
means importing from `matches.csv` (or `season.json` schedule entries) into
a new `schedule` table:

```sql
CREATE TABLE IF NOT EXISTS schedule (
    id INTEGER PRIMARY KEY,
    season_id INTEGER NOT NULL REFERENCES seasons(id),
    week INTEGER NOT NULL,
    date TEXT NOT NULL,
    home_team_id INTEGER NOT NULL REFERENCES teams(id),
    away_team_id INTEGER NOT NULL REFERENCES teams(id),
    venue_id INTEGER NOT NULL REFERENCES venues(id)
);
```

All other pages reuse existing CLI queries against the current schema.

## Future Work

1. **Match planning (`plan` command)**: Given a venue and opponent, recommend
   optimal machine picks and player-to-machine assignments across all four
   rounds. The core challenge is a constraint satisfaction problem — 10
   players, 22 games, participation bonus requirements, and the asymmetry
   between picking rounds (you choose machines and players) and responding
   rounds (you only choose players).
2. **Recent form weighting**: Weight recent games higher than old games
3. **Doubles pair recommendations**: Who partners well together
4. **Opponent tendency prediction**: ML-based pick prediction
5. **Live match tracking**: Real-time score updates and adjusted predictions

## Open Questions

1. **Sample size thresholds**: How many games before data is "reliable"?
   Suggest: Show all data but flag low sample sizes (< 3 games).

2. **Season scoping**: Should stats be all-time or current season?
   Suggest: Default to all-time, flag for `--season N` to filter.

3. **Team key vs name**: Commands use team key (CRA) for brevity. Should also
   support full names? Probably yes, with fuzzy matching.

4. **Playoff schedule**: Regular season schedule is in `matches.csv` and
   `season.json`. Playoff brackets may use a different format or not appear
   in the archive until they're scheduled. The standalone Matchup page
   serves as a fallback when the schedule doesn't cover a match.
