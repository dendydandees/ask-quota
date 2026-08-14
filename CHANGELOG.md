# Changelog

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
