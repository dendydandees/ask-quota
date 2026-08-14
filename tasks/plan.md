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

- [x] Task 1: Repo scaffold and `--version`
- [x] Task 2: Transcript scanner with dedupe and concurrency
- [x] Task 3: Cost weighting and share percentages
- [x] Task 4: Table output, top N, naive rolling window

**Checkpoint A** ✅ — `go run ./cmd/ask-quota` prints the top 5 sessions of the
last 5 hours from the real corpus, and the totals match a hand-audit of one
transcript. Review before proceeding.

### Phase 2: Correct behaviour

- [x] Task 5: Window resolution and block inference
- [x] Task 6: Optional quota source and the `QUOTA` column
- [x] Task 7: Flag parsing, validation and exit codes
- [x] Task 8: Grouping by session or project

**Checkpoint B** ✅ — every flag in the spec works, `--json` pipes cleanly into
`jq`, and the tool runs correctly on a machine with no quota source present.

### Phase 3: Ship

- [x] Task 9: Performance verification on the real corpus — 531 files / 663 MB:
      `30d --top all` in 551–740ms against a 5s target, no code changed
- [x] Task 10: `--help` text and man page
- [x] Task 11: Release scaffolding

**Checkpoint C** ✅ — all nine Success Criteria in the spec are met.

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

---

# Phase 4: drop the external quota source

Spec: `SPEC.md` (not `docs/spec.md`, which describes the shipped tool).

Replace the `quota-axi` subprocess with an in-process reader, so official
window boundaries need no Node.js and no second install step. `quota.Lookup`
keeps its signature throughout: every task below changes only its body, so the
report, the window resolver and `cmd/` are untouched until Task 15's docs.

Ordered so the network lands last and alone. Tasks 12 and 13 are pure
functions over bytes — a wrong reset time is the failure that would be
invisible, so both are fully tested before anything calls them for real.

### ✅ Task 12: Read the OAuth credentials

**Description:** Load Claude Code's stored token from disk. No network.

**Acceptance criteria:**
- [x] Reads `$CLAUDE_CONFIG_DIR/.credentials.json`, defaulting to `~/.claude`
- [x] Accepts the token at `claudeAiOauth.accessToken`, or `accessToken` /
      `access_token` at the top level
- [x] `expiresAt` is milliseconds; a past value yields no token and a reason
- [x] An absent `expiresAt` is not treated as expired
- [x] Missing, unreadable or malformed file yields a reason, never a panic
- [x] The reason is safe to print: it never contains the token

**Verification:** `go test ./internal/quota/ -run TestCredentials`
**Dependencies:** none
**Files:** `internal/quota/credentials.go`, `internal/quota/credentials_test.go`
**Scope:** S

### ✅ Task 13: Fetch and parse the usage endpoint

**Description:** `GET /api/oauth/usage`, normalise to `Official`. Behind
`httptest` in tests; no live call ever runs in CI.

**Acceptance criteria:**
- [x] Prefers the `limits` array (`group: session|weekly`, `percent`,
      `resets_at`); falls back to `five_hour`/`seven_day` with `utilization`
- [x] Model-scoped entries are ignored, not mistaken for a window
- [x] 401/403/429/5xx, malformed JSON and an oversized body each yield
      `(zero, false)` plus a distinct reason
- [x] Response body is bounded and the whole call respects an 8s budget
- [x] Redirects away from `anthropic.com` are refused

**Verification:** `go test ./internal/quota/ -run TestUsage`
**Dependencies:** Task 12
**Files:** `internal/quota/usage.go`, `internal/quota/usage_test.go`
**Scope:** M

### ✅ Task 14: Swap Lookup over and delete the subprocess

**Description:** Wire 12 and 13 into `Lookup`; remove `exec`, `cappedBuffer`
and `WaitDelay`. Honour `ASK_QUOTA_OFFLINE=1`. Replace the install hint with
the actual reason a run degraded.

**Acceptance criteria:**
- [x] `go.mod` still has no requires; `os/exec` no longer imported by `quota`
- [x] Output is unchanged for every existing test
- [x] `ASK_QUOTA_OFFLINE=1` sends no request
- [x] A degraded run names its reason on stderr, never silently

**Verification:** `go test ./...`, then a manual run with the network down
**Dependencies:** Task 13
**Files:** `internal/quota/quota.go`, `cmd/ask-quota/main.go`, tests
**Scope:** M

### ✅ Task 15: Docs and a live parity check

**Description:** Tell the truth in the docs: no external tool, one network
call. Add the `-tags=live` test that diffs against `quota-axi` while it is
still installed.

**Acceptance criteria:**
- [x] `README.md` drops the `npm install -g quota-axi` section and states the
      network call and `ASK_QUOTA_OFFLINE`
- [x] `man/ask-quota.1` ENVIRONMENT rewritten; `CHANGELOG.md` entry added
- [x] `-tags=live` parity test exists and is excluded from CI

**Dependencies:** Task 14
**Files:** `README.md`, `man/ask-quota.1`, `CHANGELOG.md`, `.github/`
**Scope:** S

### ✅ Task 16: Cache the response briefly

**Description:** Found while running Task 15's parity check: the endpoint rate
limits quickly, and ask-quota now calls it on every run where the subprocess
used to serve a cached answer. Repeated runs would degrade to `inferred` —
exactly the failure this phase set out to remove.

One GET returns every window, so the cache is keyed by nothing: a single
payload serves `5h` and `week` alike.

**Acceptance criteria:**
- [x] A second lookup within the TTL sends no request
- [x] A lookup after the TTL sends a fresh one
- [x] One cached payload answers both `5h` and `week`
- [x] An unreadable, corrupt or unwritable cache never fails a run
- [x] Only a payload that parsed is cached
- [x] The cache file never contains the token, and is written 0600

**Verification:** `go test ./internal/quota/ -run TestCache`
**Dependencies:** Task 14
**Files:** `internal/quota/cache.go`, `internal/quota/cache_test.go`,
`internal/quota/usage.go`
**Scope:** S

### ✅ Checkpoint D

- [x] `quota-axi` uninstalled; `5h` and `week` still report `official`
- [x] `go vet ./...` clean, `gofmt -l .` empty, `go.mod` still bare
- [x] The access token appears in no output stream
