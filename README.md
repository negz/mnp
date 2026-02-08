# mnp

`mnp` is a scouting tool for Seattle's [Monday Night Pinball] league. It syncs
match results, rosters, venues, and machines from the MNP data archive into a
local SQLite database, then answers questions players might ask before match
night: what machines should we pick, who should play them, and what will the
opponent likely do.

## Commands

| Command | Purpose |
|---------|---------|
| `scout <team>` | Team strengths and weaknesses across all machines |
| `matchup <venue> <t1> <t2>` | Head-to-head comparison at a venue |
| `recommend <team> <machine>` | Who should play a specific machine |
| `player <name>` | Individual player stats across machines |
| `teams` | List teams with home venues |
| `venues` | List venues |
| `machines` | List machines |
| `serve` | Start the web UI |

Most commands accept `--venue` to filter stats to a specific location. The
`recommend` command accepts `--vs` to compare against an opponent's likely
players.

### Examples

Scout a team's profile:

```
mnp scout TTT
```

Compare two teams at a venue:

```
mnp matchup STN TTT KNR
```

See who should play Total Nuclear Annihilation, and how they stack up against
the opponent:

```
mnp recommend TTT TNA --vs KNR
```

Look up an individual player:

```
mnp player "Nic Cope"
```

## Data sync

MNP pulls data from a Git-hosted archive of league results. It syncs
automatically on first use and before each command. The web UI re-syncs every
24 hours. The database and cloned repo live in `$XDG_CACHE_HOME/mnp` (defaults
to `~/.cache/mnp`).

## Web UI

`mnp serve` starts an HTTP server that mirrors the CLI commands with a
schedule-driven landing page. Pick your team and see upcoming matches with
pre-filled links to matchup and scout pages. All state is in the URL, so pages
are shareable.

```
mnp serve --addr :8080
```

## Install

```
go install github.com/negz/mnp/cmd/mnp@latest
```

Or build from source:

```
go build -o mnp ./cmd/mnp
```

## License

Apache 2.0

[Monday Night Pinball]: https://www.mondaynightpinball.com
