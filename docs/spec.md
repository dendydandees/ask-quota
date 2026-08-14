# Spec: ask-quota

## Objective

A CLI that answers **"where did my quota go?"** by breaking usage down per task,
read from local Claude Code transcripts.

Existing tools report *how much is left* (a single percentage). None report
*what consumed it*. `ask-quota` fills that gap.

- **User:** any Claude Code user on Linux, installed as a single binary.
- **Success:** on a machine with nothing installed but Claude Code,
  `ask-quota` with no flags prints a correct, useful answer.
- **Non-goal:** replacing quota-reporting tools. `ask-quota` explains
  composition, not remaining balance.

### Assumptions

1. **Linux only.** No macOS or Windows support is promised. The code stays
   portable where it costs nothing, but nothing is tested elsewhere.
2. Transcripts live at `~/.claude/projects/**/*.jsonl`.
3. Zero third-party dependencies. Go standard library only.
4. Go >= 1.24. Tests use `testing`.
5. MIT. Distributed as a static binary via GitHub Releases, plus `go install`.

## Tech Stack

- Go >= 1.24, standard library only, no third-party modules
- `testing` + table-driven tests
- man page in troff (`man/ask-quota.1`)

Chosen over Node/Python because mode `30d` touches ~530 files / ~660 MB;
per-file goroutines keep that at ~2s, and a static binary means the user
installs one file with no runtime at all.

**No CLI framework.** `flag` from the standard library covers the whole
surface: one command, four flags, one positional. Cobra, urfave/cli and kong
were considered and rejected — they pay for themselves on subcommand trees,
and this tool has none. Cobra's man-page generator was the only real draw, but
it emits a flag list, while the sections that matter here (LIMITATIONS, and
the caveat that QUOTA is a proportion rather than a measurement) are prose no
generator can write. Revisit only if a real subcommand appears; migrating then
is an hour of work, not a rewrite.

## Commands

```
Run:      go run ./cmd/ask-quota
Test:     go test ./...
Lint:     go vet ./... && gofmt -l .
Build:    go build -trimpath -ldflags="-s -w" -o dist/ask-quota ./cmd/ask-quota
Install:  go install ./cmd/ask-quota
Man:      man ./man/ask-quota.1        # preview before release
```

## Project Structure

```
cmd/ask-quota/main.go       → entry: wire flags, run, map errors to exit codes
internal/cli/flags.go       → parse + validate argv into a Config
internal/cli/help.go        → --help text (kept in sync with the man page)
internal/window/window.go   → resolve a window to an absolute start instant
internal/window/infer.go    → block-start inference from activity gaps
internal/transcript/scan.go → discover files, parse lines, emit sessions
internal/quota/quota.go     → optional external lookup of the official window
internal/report/cost.go     → token weighting, share/quota percentages
internal/report/group.go    → session and project grouping
internal/report/render.go   → table and JSON output
man/ask-quota.1             → man page
docs/spec.md                → this file
testdata/                   → tiny synthetic .jsonl fixtures
```

Tests live beside the code they cover (`*_test.go`), per Go convention.

## Behaviour

### Windows

`--window` (positional shorthand also accepted): `5h` | `week` | `30d`.
**Default: `5h`.**

Window start resolution, in order:

| Window | With a quota source available | Standalone fallback |
|---|---|---|
| `5h` | official reset minus 5h | **infer**: start of the current activity block — the most recent message preceded by a gap > 5h |
| `week` | official reset minus 7d | rolling: now minus 7d |
| `30d` | n/a — no official 30d window exists | rolling: now minus 30d |

The inference for `5h` was validated against a real window: official start
`04:20:00Z` vs inferred `04:28:45Z` — within ~9 minutes.

A quota source is **optional**. If none is present, the tool runs fully and
omits the official-percentage column. It never errors out over its absence.

### Filtering

Filtering is per **message timestamp**, never per file mtime — a session may
span the window boundary and only the in-window portion counts.

### Grouping

`--group-by session | project`. **Default: `session`.**

- `session` — one row per transcript, labelled by its first real user prompt
  (skipping slash-commands, hook output, caveats, and resume notices).
- `project` — rows merged by working directory, so one feature worked across
  several repos shows up as a single number.

### Ranking

`--top N` or `--top all`. **Default: `5`.** Rows sorted by cost descending.

### Numbers

The primary number is a **share percentage**, never a dollar estimate.

- `SHARE` — this row's cost as a percentage of all cost in the window.
  Always present and fully measured.
- `QUOTA` — `SHARE` scaled by the official window percentage, i.e. estimated
  percentage points of the real quota window. Shown **only** when an official
  percentage is available (`5h` and `week`; never `30d`).

Cost weight, relative to input tokens, following published price ratios:

```
cost = input + cache_write * 1.25 + cache_read * 0.10 + output * 5
```

This is a **ranking proxy, not a measurement**. It ignores per-model price
differences. The man page must say so explicitly.

### Correctness requirement

Every assistant message appears roughly three times per transcript. Rows must
be deduplicated by `message.id` before summing. Without this, every number is
inflated ~3x. This is the single highest-risk defect and requires a test.

Deduplication is **global to the window, not per file**. Resuming or compacting
a conversation writes a fresh transcript that replays earlier assistant turns
with their original ids and original timestamps, so the copies land in the same
window. Measured on a real corpus of 534 files: 926 ids appear in more than one
file, inflating the 30-day report by 6.1% and the weekly one by 2.4% — and in
one case a whole row was a strict subset of another, making it entirely phantom.

**A message belongs to the session that saw it first.** Sessions are ordered by
their earliest message, and a later session that replays an id does not count it
again. A session left with no messages of its own disappears from the report
rather than showing as a zero row. Ordering by first message rather than by walk
order is what makes the attribution deterministic and puts the usage on the
conversation that originally incurred it.

### Output

Aligned text table by default. `--json` emits an array of row objects for
piping, with no header line and no colour.

### Honesty requirement

Both `--help` and the man page must state that only transcripts on **this
machine** are visible: sessions from other machines, the web app, and cloud
agents consume the same quota but leave no local trace. Silence here would let
users read a partial number as a total.

## Code Style

Idiomatic Go: small packages, values over pointers for plain data, errors
returned not panicked. Only `cmd/` touches stdout and exit codes; every
`internal/` package takes data and returns data.

```go
// internal/report/cost.go

// Weights are relative to an input token, following published price ratios.
// Deliberately model-agnostic: this ranks sessions, it does not price them.
const (
	weightInput      = 1.0
	weightCacheWrite = 1.25
	weightCacheRead  = 0.10
	weightOutput     = 5.0
)

// Cost returns cost units for a single usage record.
func Cost(u Usage) float64 {
	return float64(u.Input)*weightInput +
		float64(u.CacheWrite)*weightCacheWrite +
		float64(u.CacheRead)*weightCacheRead +
		float64(u.Output)*weightOutput
}
```

- Exported identifiers carry doc comments starting with the name
- Comments explain *why*, never restate the code (e.g. the 3x dedupe)
- User errors print a plain message to stderr and exit non-zero; never a panic
  or a stack trace for bad input
- `gofmt` clean; scope formatting to changed files, never reformat the repo

### Performance requirement

One goroutine per file, bounded by `runtime.NumCPU()`. Before unmarshalling a
line, cheap-reject it with a substring check for `"usage"` — most lines are
user turns and tool results, and skipping them avoids the dominant cost.

Target: mode `30d` over ~530 files / ~660 MB completes in under 5 seconds on a
warm cache.

## Testing Strategy

`go test ./...`, table-driven where the cases are data. Fixtures are tiny
hand-written `.jsonl` files under `testdata/`.

Required cases:

| Concern | Test |
|---|---|
| Dedupe | a fixture with one message repeated 3x sums once |
| Window boundary | messages before window start are excluded |
| Block inference | a fixture with a >5h gap infers the later block start |
| No quota source | runs successfully, omits the `QUOTA` column |
| `--top all` | returns every row, not 5 |
| Grouping | two sessions sharing a cwd merge into one project row |
| Arg errors | `--top -3` and `--window 9y` exit non-zero with a clear message |
| Empty state | no transcripts found prints guidance, exits 0 |

No coverage threshold. Every behaviour listed above must have a test; internals
need none.

## Boundaries

- **Always:** keep `internal/` free of I/O except `internal/quota`, whose whole
  job is the external lookup; run `go test ./...` before committing; keep `--help` and the man
  page in sync; state the local-only limitation in user-facing docs.
- **Ask first:** adding any third-party module; cutting a release; adding a
  second provider; changing the cost weights.
- **Never:** print a dollar figure; require an external binary to function;
  read or transmit transcript *content* anywhere off the machine.

## Success Criteria

1. The downloaded binary, run with no flags on a Linux box with no other
   tooling installed, prints the top 5 sessions of the current 5-hour block.
2. Removing the optional quota source changes only the `QUOTA` column.
3. `--json | jq` works with no header contamination.
4. `ask-quota --help` fits on one screen (< 40 lines) and names every flag.
5. `man ask-quota` renders with NAME, SYNOPSIS, DESCRIPTION, OPTIONS,
   EXAMPLES, LIMITATIONS.
6. `go test ./...` passes, covering every case in the table above.
7. Numbers match a hand-audited fixture exactly — specifically, no 3x
   inflation.
8. `30d` over the full corpus (~530 files, ~660 MB) finishes in under 5s.
9. `go vet ./...` clean and `gofmt -l .` empty.

## Open Questions

None blocking. Deferred by decision: other providers, dollar estimates,
per-model weighting, cross-machine aggregation.
