# Tasks: ask-quota

Plan: `tasks/plan.md`. Spec: `docs/spec.md`.

Every task also clears the Definition of Done in the plan.

---

## Phase 1: Walking skeleton

### Task 1: Repo scaffold and `--version`

**Description:** Create the Go module and a binary that builds, runs, and
reports its version. Nothing else. This exists so every later task has a
working build to land into.

**Acceptance criteria:**
- [ ] `go build ./...` succeeds; module path is `github.com/<user>/ask-quota`
- [ ] `ask-quota --version` prints a version string and exits 0
- [ ] `go.mod` declares Go >= 1.24 and has an empty require block

**Verification:**
- [ ] Build: `go build -o dist/ask-quota ./cmd/ask-quota`
- [ ] Manual: `./dist/ask-quota --version`

**Dependencies:** None
**Files:** `go.mod`, `cmd/ask-quota/main.go`, `.gitignore`
**Scope:** XS

---

### Task 2: Transcript scanner with dedupe and concurrency

**Description:** Walk `~/.claude/projects/**/*.jsonl`, parse usage records,
and emit one aggregated row per transcript. This is the highest-risk task in
the project: every assistant message appears ~3x per file, so summing without
deduplicating by `message.id` inflates every number in the tool by ~3x while
still looking plausible. Concurrency belongs here rather than in a later pass —
this is the module that touches the filesystem, and retrofitting it later means
rewriting it.

**Acceptance criteria:**
- [ ] Emits per-file totals for input, output, cache-write, cache-read tokens
- [ ] Rows are deduplicated by `message.id` before summing
- [ ] Lines are cheap-rejected with a `"usage"` substring check before
      unmarshalling
- [ ] Fan-out is one goroutine per file, bounded by `runtime.NumCPU()`
- [ ] A malformed line is skipped, not fatal — transcripts are append-only logs
      and can end mid-write

**Verification:**
- [ ] Tests: `go test ./internal/transcript/`
- [ ] Fixture: one message repeated 3x sums exactly once
- [ ] Fixture: a truncated final line does not fail the scan
- [ ] Manual: totals for one real transcript match a hand-audited `jq` count

**Dependencies:** Task 1
**Files:** `internal/transcript/scan.go`, `internal/transcript/scan_test.go`,
`testdata/*.jsonl`
**Scope:** M

---

### Task 3: Cost weighting and share percentages

**Description:** Convert token counts into comparable cost units and compute
each row's share of the window total. Pure arithmetic over the scanner's
output, no I/O.

**Acceptance criteria:**
- [ ] `Cost()` applies the weights fixed in the spec (1 / 1.25 / 0.10 / 5)
- [ ] `SHARE` is a row's cost over the sum of all rows' cost
- [ ] Shares sum to 100% (within float tolerance) across all rows
- [ ] An empty input set returns no rows and does not divide by zero

**Verification:**
- [ ] Tests: `go test ./internal/report/`
- [ ] Table test covering the weights and the empty case

**Dependencies:** Task 2
**Files:** `internal/report/cost.go`, `internal/report/cost_test.go`
**Scope:** S

---

### Task 4: Table output, top N, naive rolling window

**Description:** Close the loop end to end: filter by a hardcoded "now minus 5
hours" window, sort by cost, print an aligned table of the top 5. The naive
window is deliberate scaffolding, replaced in Task 5 — it exists so the
skeleton runs against real data before the hardest logic is written.

**Acceptance criteria:**
- [ ] Filtering is by message timestamp, never by file mtime
- [ ] Rows sorted by cost descending, top 5 shown
- [ ] Columns align regardless of value width; long labels are truncated, and
      truncation never breaks alignment
- [ ] Session label is the first real user prompt, skipping slash-commands,
      hook output, caveats and resume notices

**Verification:**
- [ ] Tests: `go test ./internal/report/`
- [ ] Manual: `go run ./cmd/ask-quota` prints a plausible table from the real
      corpus

**Dependencies:** Task 3
**Files:** `internal/report/render.go`, `internal/report/render_test.go`,
`cmd/ask-quota/main.go`
**Scope:** M

---

### ✅ Checkpoint A

- [ ] `go test ./...` passes
- [ ] Top 5 sessions of the last 5 hours print from the real corpus
- [ ] Hand-audit of one transcript matches the tool's number exactly (no 3x)
- [ ] **Review with human before Phase 2**

---

## Phase 2: Correct behaviour

### Task 5: Window resolution and block inference

**Description:** Replace the naive window. Resolve `5h`, `week` and `30d` to an
absolute start instant, with a standalone fallback when no quota source is
available. The `5h` fallback infers the current activity block: the most recent
message preceded by a gap larger than 5 hours. Second-highest risk in the
project, because a wrong window is silently wrong in the default mode.

**Acceptance criteria:**
- [ ] `5h` with a quota source: official reset minus 5h
- [ ] `5h` without one: inferred block start from the activity gap
- [ ] `week`: official reset minus 7d, else rolling now minus 7d
- [ ] `30d`: always rolling now minus 30d; no official percentage exists
- [ ] Inference over the captured real window lands within ~15 minutes of the
      official start (recorded: official `04:20:00Z`, inferred `04:28:45Z`)

**Verification:**
- [ ] Tests: `go test ./internal/window/`
- [ ] Fixture: timestamps with a >5h gap infer the later block
- [ ] Fixture: no gap at all falls back to the oldest message, not an error
- [ ] Manual: compare inferred `5h` start against the live official reset

**Dependencies:** Task 4
**Files:** `internal/window/window.go`, `internal/window/infer.go`,
`internal/window/window_test.go`, `testdata/gaps.jsonl`
**Scope:** M

---

### Task 6: Optional quota source and the `QUOTA` column

**Description:** Read the official window percentage if a quota source is
present, and render `QUOTA` as `SHARE` scaled by it. Absence must degrade
exactly one column and nothing else — no error, no warning that looks like a
failure, no changed exit code.

**Acceptance criteria:**
- [ ] `QUOTA` appears only when an official percentage is available
- [ ] Never shown for `30d`, since no 30-day window exists upstream
- [ ] A missing, failing or slow source is handled with a timeout and treated
      as simply absent
- [ ] Without the source, output is identical minus that column

**Verification:**
- [ ] Tests: `go test ./internal/report/`
- [ ] Manual: run with the source present, then with `PATH` stripped of it —
      diff shows only the `QUOTA` column

**Dependencies:** Task 5
**Files:** `internal/report/quota.go`, `internal/report/quota_test.go`,
`internal/report/render.go`
**Scope:** S

---

### Task 7: Flag parsing, validation and exit codes

**Description:** Wire the real CLI surface with stdlib `flag`: `--window`,
`--top`, `--group-by`, `--json`, plus the positional window shorthand. Bad
input gets a plain message and a non-zero exit, never a panic or a stack
trace.

**Acceptance criteria:**
- [ ] Defaults: `--window 5h`, `--top 5`, `--group-by session`
- [ ] Positional shorthand works: `ask-quota week`
- [ ] `--top all` returns every row; `--top 0` and `--top -3` are errors
- [ ] Invalid `--window` / `--group-by` values list the valid ones
- [ ] `--json` emits an array of row objects only — no header line, no colour
- [ ] Errors go to stderr and exit 2; empty results print guidance and exit 0

**Verification:**
- [ ] Tests: `go test ./internal/cli/`
- [ ] Table test over valid and invalid argv combinations
- [ ] Manual: `ask-quota --json | jq '.[0]'` parses

**Dependencies:** Task 4
**Files:** `internal/cli/flags.go`, `internal/cli/flags_test.go`,
`cmd/ask-quota/main.go`
**Scope:** M

---

### Task 8: Grouping by session or project

**Description:** Add `--group-by project`, merging rows by working directory
so one feature worked across several repos reads as a single number instead of
several small ones.

**Acceptance criteria:**
- [ ] `session` (default) is unchanged from Phase 1
- [ ] `project` merges rows sharing a working directory and sums their costs
- [ ] Project label is a readable short path, not an absolute home path
- [ ] Shares still sum to 100% after grouping

**Verification:**
- [ ] Tests: `go test ./internal/report/`
- [ ] Fixture: two sessions sharing a cwd merge into one row
- [ ] Manual: total cost is identical under both grouping modes

**Dependencies:** Task 7
**Files:** `internal/report/group.go`, `internal/report/group_test.go`
**Scope:** S

---

### ✅ Checkpoint B

- [ ] `go test ./...` passes
- [ ] Every flag in the spec behaves as specified
- [ ] `--json | jq` works with no header contamination
- [ ] Running without a quota source changes only the `QUOTA` column
- [ ] **Review with human before Phase 3**

---

## Phase 3: Ship

### Task 9: Performance verification on the real corpus

**Description:** Measure `30d` against the real corpus (~530 files, ~660 MB)
and tune only if the target is missed. No speculative optimisation — the
concurrency from Task 2 is expected to be sufficient.

**Acceptance criteria:**
- [ ] `30d` over the full corpus completes in under 5s on a warm cache
- [ ] Memory stays bounded — files stream, never fully buffered
- [ ] If the target is met, nothing is changed

**Verification:**
- [ ] Manual: `time ./dist/ask-quota 30d --top all`
- [ ] Benchmark recorded in the task notes for future comparison

**Dependencies:** Task 8
**Files:** `internal/transcript/scan.go` (only if tuning proves necessary)
**Scope:** S

---

### Task 10: `--help` text and man page

**Description:** Write the user-facing documentation, in English. Both must
state the local-only limitation: sessions from other machines, the web app and
cloud agents consume the same quota but leave no local trace. Both must also
say that `QUOTA` is a proportion, not a measurement.

**Acceptance criteria:**
- [ ] `--help` is under 40 lines, names every flag, shows defaults
- [ ] `man/ask-quota.1` has NAME, SYNOPSIS, DESCRIPTION, OPTIONS, EXAMPLES,
      LIMITATIONS
- [ ] Both state the local-only limitation and the `QUOTA` caveat
- [ ] Flag names and defaults in both match `internal/cli/flags.go` exactly

**Verification:**
- [ ] Manual: `ask-quota --help` fits one screen
- [ ] Manual: `man ./man/ask-quota.1` renders without troff warnings
- [ ] Manual: cross-read both against the flag definitions

**Dependencies:** Task 7
**Files:** `internal/cli/help.go`, `man/ask-quota.1`
**Scope:** M

---

### Task 11: Release scaffolding

**Description:** Make the project installable by a stranger: git history,
licence, README, and a reproducible build command.

**Acceptance criteria:**
- [ ] `git init`, initial commit, MIT `LICENSE`
- [ ] `README.md` shows install, one example invocation with real output, and
      the local-only limitation
- [ ] Build is reproducible: `-trimpath -ldflags="-s -w"`
- [ ] Version string is injected at build time, not hardcoded

**Verification:**
- [ ] Manual: build, copy the binary to a clean `PATH`, run with no flags
- [ ] Manual: README example matches actual output

**Dependencies:** Task 10
**Files:** `README.md`, `LICENSE`, `Makefile`, `cmd/ask-quota/main.go`
**Scope:** S

---

### ✅ Checkpoint C

- [ ] All nine Success Criteria in `docs/spec.md` verified
- [ ] `go vet ./...` clean, `gofmt -l .` empty
- [ ] `go.mod` still has no third-party requires
- [ ] Binary runs on a machine with nothing else installed
