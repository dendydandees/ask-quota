# Implementation Plan: ask-quota

Spec: `docs/spec.md`. Read it first; this document does not repeat it.

## Overview

A Linux CLI, written in Go with the standard library only, that reads local
Claude Code transcripts and reports which tasks consumed the quota window.
Ships as a single static binary.

The build is ordered as a walking skeleton: get real numbers on screen from
the real corpus as early as possible, then make the windows, flags and output
correct around that spine.

## Architecture Decisions

- **`cmd/` owns all I/O.** Every `internal/` package takes data and returns
  data, so output modes can be tested by comparing structs rather than
  capturing stdout.
- **Concurrency lives in the scanner only.** One goroutine per file bounded by
  `runtime.NumCPU()`, added in Task 2 rather than retrofitted — the scanner is
  its natural home, and bolting it on later would mean rewriting the module.
- **The optional quota source is a `nil`-able value, not a branch everywhere.**
  Absence degrades one column; it never changes control flow.
- **A naive rolling window ships first (Task 4), then is replaced by real
  window resolution (Task 5).** This keeps Phase 1 runnable end to end without
  waiting on the hardest logic in the project.

## Risk Order

Both high-risk tasks land before any polish, because both fail *silently*:

| Risk | Impact | Mitigation |
|---|---|---|
| Dedupe missed → every number inflated ~3x | **High** — output looks plausible while being wrong | Task 2, first real task; fixture with a message repeated 3x; hand-audited number in Success Criteria #7 |
| Block inference wrong → `5h` reports the wrong window | **High** — silently wrong default mode | Task 5; verified against a captured real window (official `04:20:00Z` vs inferred `04:28:45Z`) |
| Perf unusable in `30d` | Medium — mode gets abandoned | Concurrency built into Task 2; measured against the real 660 MB corpus in Task 9 |
| `--help` and man page drift apart | Low | Single source of truth in `internal/cli/help.go`; man page reviewed in the same task |

## Task List

### Phase 1: Walking skeleton

- [ ] Task 1: Repo scaffold and `--version`
- [ ] Task 2: Transcript scanner with dedupe and concurrency
- [ ] Task 3: Cost weighting and share percentages
- [ ] Task 4: Table output, top N, naive rolling window

**Checkpoint A** — `go run ./cmd/ask-quota` prints the top 5 sessions of the
last 5 hours from the real corpus, and the totals match a hand-audit of one
transcript. Review before proceeding.

### Phase 2: Correct behaviour

- [ ] Task 5: Window resolution and block inference
- [ ] Task 6: Optional quota source and the `QUOTA` column
- [ ] Task 7: Flag parsing, validation and exit codes
- [ ] Task 8: Grouping by session or project

**Checkpoint B** — every flag in the spec works, `--json` pipes cleanly into
`jq`, and the tool runs correctly on a machine with no quota source present.

### Phase 3: Ship

- [ ] Task 9: Performance verification on the real corpus
- [ ] Task 10: `--help` text and man page
- [ ] Task 11: Release scaffolding

**Checkpoint C** — all nine Success Criteria in the spec are met.

## Parallelization

Mostly sequential; the graph is narrow. Genuinely independent once Task 4
lands: Task 6, Task 8, and Task 10 touch disjoint files and could run in
parallel. Task 2 → 3 → 4 must be sequential (each consumes the previous
package's types).

## Definition of Done

Standing bar for every task, on top of its own acceptance criteria:

- `go test ./...` passes
- `go vet ./...` clean, `gofmt -l .` empty (scope formatting to changed files)
- No third-party module added to `go.mod`
- User-facing text stays honest about the local-only limitation

## Open Questions

None. Deferred by decision: other providers, dollar estimates, per-model
weighting, cross-machine aggregation.
