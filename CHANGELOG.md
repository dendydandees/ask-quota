# Changelog

## v0.2.0 — 2026-08-14

### Changed

- Official window boundaries are now read in process, from
  `https://api.anthropic.com/api/oauth/usage`, using the OAuth token Claude Code
  already stores. The external `quota-axi` command is no longer executed or
  needed, and Node.js is not involved: nothing beyond the binary has to be
  installed for the QUOTA column to appear.
- A run that falls back to a guess now says why on stderr — expired sign-in,
  rate limit, unreachable network — instead of dropping the QUOTA column
  silently.

### Security

- Builds are pinned to Go 1.25.13. Speaking TLS put `crypto/tls`, `crypto/x509`
  and `net/http` on the call path for the first time, and `govulncheck` found 25
  reachable standard-library vulnerabilities on an older toolchain. CI now runs
  `make audit` so a stale pin fails the build rather than going unnoticed.

### Fixed

- The 30-day window reported its boundary as `inferred`. It has no upstream
  equivalent and never asks for one: it is a plain span back from now, exact by
  definition, and is now labelled `rolling`. The 7-day fallback was mislabelled
  the same way.

### Added

- `ASK_QUOTA_OFFLINE=1` skips the quota lookup, restoring the property that the
  tool makes no network calls at all.
- The quota response is cached for five minutes under `~/.cache/ask-quota`, so
  a burst of runs costs one request rather than tripping the endpoint's rate
  limit and degrading to a guess. The file holds no credentials.
- `make live-update` records the real payload to
  `internal/quota/testdata/usage-live.json` with volatile values scrubbed. An
  upstream shape change then shows up as a diff before a release.

## v0.1.0 — 2026-08-14

First release.

### Added

- Report which sessions consumed a quota window, read from local Claude Code
  transcripts under `~/.claude/projects`.
- Windows `5h` (default), `week` and `30d`. The 5-hour and 7-day boundaries come
  from an external quota source when one is installed; without it the current
  5-hour block is inferred from the transcripts themselves, within about nine
  minutes of the real boundary in testing.
- `--top N | all` (default 5), `--group-by session | project` (default session),
  and `--json` for piping.
- `SHARE`, measured entirely from local data, and `QUOTA`, that share scaled by
  the officially reported percentage used — shown only when an official figure
  is available, and documented as a proportion rather than a measurement.
- Man page and `--help`, both stating that only transcripts on this machine are
  visible.

### Notes on correctness

The numbers depend on three deduplication rules that were each wrong at some
point during development, and each produced plausible-looking output while wrong:

- An assistant message is written about three times per transcript. Counting
  every copy inflates all numbers threefold.
- Those copies are streaming snapshots, not repeats. Keeping the first
  undercounted output by 7.5% on a real corpus.
- Resuming a conversation replays earlier turns with their original ids and
  timestamps into a new transcript. Counting them again inflated a 30-day report
  by 6.1%; awarding the replayed turn to the earlier session without comparing
  the copies reintroduced the undercount at the file boundary.

Against a ground truth of globally unique ids at their most complete copy, the
weekly total is within 0.1%.
