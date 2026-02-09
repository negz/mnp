# Contributing to MNP

MNP (Monday Night Pinball) is a tool for Seattle's Monday Night Pinball league
data. It imports data from the MNP Data Archive, stores it in SQLite, and
provides a CLI and web UI for strategic analysis.

## Problem Context

Team captains preparing for weekly matches need answers to predictable
questions: What machines should we pick? Who should play each machine? What will
the opponent likely pick?

The raw data exists in the MNP Data Archive, but answering these questions
requires SQL knowledge and understanding of schema quirks. MNP hides this
complexity behind commands that surface the right metrics — P50/P90 scores and
relative strength rather than win rate, which is polluted by opponent strength.

### Match Format

Each match has 4 rounds. The picking team selects machines AND their players;
the responding team only chooses who to send.

| Round | Type              | Picking Team | Responding Team |
|-------|-------------------|--------------|-----------------|
| R1    | Doubles (4 games) | Away         | Home            |
| R2    | Singles (7 games) | Home         | Away            |
| R3    | Singles (7 games) | Away         | Home            |
| R4    | Doubles (4 games) | Home         | Away            |


## Contributing

Pull requests are welcome. Please open an issue first to discuss what you'd like
to change and how you'd approach it — this avoids duplicating work or heading in
a direction that won't be accepted.

## Development

### Build, Test, Lint

```bash
go build -o mnp ./cmd/mnp
go test ./...
golangci-lint run
```

The linter uses golangci-lint v2 (`.golangci.yml`) with `default: all` and a
curated disable list. It runs formatters (gci, gofmt, gofumpt, goimports) and
enforces most style rules automatically.

### CI

GitHub Actions (`.github/workflows/ci.yaml`) runs lint, test, and cross-platform
builds on every push and PR. Releases are triggered via `workflow_dispatch` with
a version input — this builds binaries, creates a GitHub release, pushes a
container image to GHCR, and deploys to Fly.io.

## Architecture

CLI commands live under `cmd/mnp/` and use [Kong](https://github.com/alecthomas/kong).
Analysis logic lives in `internal/strategy/` — each sub-package defines its own
`Store` interface with only the database methods it needs. CLI commands and web
handlers call these packages, then format results for their respective output
(ASCII tables for CLI, HTML templates for web).

## Coding Style

We follow the style and best practices established by the Go project and its
community. Contributors should be familiar with:

- [Effective Go](https://golang.org/doc/effective_go)
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
- [Go Test Comments](https://go.dev/wiki/TestComments)

The Crossplane project's [contributing guide] covers these conventions in depth
with worked examples. MNP follows the same coding style. Below are the rules
that come up most often and aren't caught by the linter.

[contributing guide]: https://github.com/crossplane/crossplane/blob/main/contributing/README.md#coding-style

### Short Variable Names

Short names are preferred when the type or context makes the meaning clear.
Longer names are fine when they aren't redundant with the type.

```go
// Good — the type tells you what s is.
func (s *SQLiteStore) ListTeams(ctx context.Context) ([]Team, error) {

// Bad — the name repeats the type.
func (sqliteStore *SQLiteStore) ListTeams(ctx context.Context) ([]Team, error) {
```

### Return Early

Handle errors and terminal cases first so the main logic isn't nested.

```go
// Good
if err != nil {
        return fmt.Errorf("fetch player: %w", err)
}
// ... main logic at function scope ...

// Bad
if err == nil {
        // ... main logic nested inside a conditional ...
} else {
        return fmt.Errorf("fetch player: %w", err)
}
```

An `else` after a `return` is almost always unnecessary.

### Scope Errors

Keep `err` scoped to the conditional block when the success value isn't needed
afterward.

```go
// Good — err doesn't leak to function scope.
if err := s.db.ExecContext(ctx, query); err != nil {
        return fmt.Errorf("exec query: %w", err)
}

// Fine — v is needed after the check, so function-scoped err is acceptable.
v, err := fetch()
if err != nil {
        return fmt.Errorf("fetch: %w", err)
}
store(v)
```

### Wrap Errors

Always wrap errors with context using `fmt.Errorf`. The message should describe
what was being attempted, in lowercase without a trailing period.

```go
if err != nil {
        return fmt.Errorf("get team machine stats: %w", err)
}
```

### Nolint Directives

Must be specific and include an explanation.

```go
defer f.Close() //nolint:errcheck // Nothing to do with error on program exit.
```

### Table-Driven Tests

Tests use the standard `testing` package with `google/go-cmp` for assertions.
No third-party test frameworks (testify, ginkgo) — see the [Go Test
Comments](https://go.dev/wiki/TestComments) rationale.

```go
func TestExample(t *testing.T) {
        cases := map[string]struct {
                input string
                want  int
        }{
                "BriefPascalCaseName": {
                        input: "something",
                        want:  42,
                },
        }

        for name, tc := range cases {
                t.Run(name, func(t *testing.T) {
                        got := Example(tc.input)
                        if diff := cmp.Diff(tc.want, got); diff != "" {
                                t.Errorf("Example(%q): -want, +got:\n%s", tc.input, diff)
                        }
                })
        }
}
```

Test case names are PascalCase with no spaces or underscores.

### Import Order

Enforced by gci — standard library, then third-party, then project packages:

```go
import (
        "context"
        "fmt"

        "github.com/alecthomas/kong"

        "github.com/negz/mnp/internal/db"
)
```

## Gotchas

1. **JSON struct tags** in `internal/mnp/` must match external data formats.
   Don't rename them.

2. **Data format assumptions** in `internal/mnp/` must match the upstream MNP
   Data Archive. Don't modify these without understanding the external format.

