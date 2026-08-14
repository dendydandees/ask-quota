# ask-quota

See which tasks consumed your Claude Code quota.

Quota tools tell you how much is left — a single number, `57% used`. They never
tell you what consumed it. `ask-quota` reads your local Claude Code transcripts
and breaks that number down per task.

```
$ ask-quota week --group-by project
window week: since 2026-08-09T12:59:59+07:00 (official, 58% used)
SHARE  QUOTA  OUT   CACHE_W  CACHE_R  PROJECT                                    TASK
22.9%  13.3%  500k  3.4M     188.7M   monolith/feat-change-package-with-payment  6 sessions
17.1%  9.9%   460k  2.2M     141.1M   monolith/feat-appt-draft-pic-kpi           6 sessions
10.1%  5.8%   217k  1.0M     89.1M    fisiohome-backend/feat-appt-draft-pic-kpi  4 sessions
8.7%   5.1%   189k  779k     78.3M    monolith/hotfix-create-booking-validation  1 session
8.3%   4.8%   165k  621k     76.7M    monolith/fix-admin-pic                     4 sessions
```

Two features accounted for 23 of the 58 points consumed that week — and the
second one only looks large once its sessions across three repositories are
merged.

## Install

```sh
go install github.com/dendydandees/ask-quota/cmd/ask-quota@latest
```

Or download a binary from the releases page and check it against the published
`checksums.txt`:

```sh
sha256sum -c checksums.txt --ignore-missing
```

Released binaries are static and reproducible: `make dist` at the same tag, on
the pinned toolchain, produces the same bytes the release published.

From a clone, `make install` also places the man page under `~/.local/share/man`.

No runtime, no dependencies, nothing else to install: a single static binary,
Go standard library only. Linux is the supported target.

Build with **Go 1.25.13 or newer**. `ask-quota` speaks TLS, so `crypto/tls`,
`crypto/x509` and `net/http` are on its call path and an older toolchain bakes
their known vulnerabilities into the binary. Released binaries are built at the
pinned version; `go install` uses whatever toolchain you have, so check
`go version` first. `make audit` runs `govulncheck` against your build.

### The one network call

For official window boundaries and the QUOTA column, `ask-quota` asks
`api.anthropic.com` about your quota, authenticating with the token Claude Code
already stores in `~/.claude/.credentials.json`. That is the only request it
makes, and the only thing it sends: no transcript text ever leaves the machine.
The token is read, never written, refreshed or logged.

The answer is cached for five minutes under `~/.cache/ask-quota`, so a burst of
runs costs one request — the endpoint rate limits, and being limited means
falling back to a guess.

`ASK_QUOTA_OFFLINE=1` skips the call entirely. The report still ranks your
sessions from local data — see [Windows](#windows) for exactly what changes.

## Use

```sh
ask-quota                             # top 5 sessions of the current 5-hour block
ask-quota week                        # the current 7-day window
ask-quota 30d --top all               # everything from the last 30 days
ask-quota week --group-by project     # merge sessions by directory
ask-quota --json | jq                 # machine-readable
```

See `ask-quota --help` for every flag, or `man ask-quota` for the full page.

## Reading the numbers

**SHARE** is this row's percentage of the window's total, measured entirely
from local data. Shares are computed across every session in the window, so the
rows shown — the top N — sum to less than 100%.

**QUOTA** is SHARE scaled by the officially reported percentage used, so it
estimates points of the real quota window. It is a proportion, not a
measurement, and appears only when an official figure is available.

Rows rank by a cost weight from published price ratios — cache reads count for
a tenth of an input token, cache writes for 1.25, output tokens for 5. It
ranks; it does not price, and it ignores differences between models.

## Windows

The 5-hour and 7-day windows mirror the ones the service enforces. When the
quota lookup succeeds, boundaries come from it. When it does not — expired
sign-in, no network, `ASK_QUOTA_OFFLINE=1` — `ask-quota` infers the
current 5-hour block from the transcripts themselves: a block opens on a
message and closes five hours later, so the current one begins at the first
message after the previous closed. Against a live window that inference landed
within nine minutes.

The 30-day window has no upstream equivalent, so it is always the last 30 days
and never carries a QUOTA column.

The first line of every report says which of the three it was:

| | |
|---|---|
| `official` | the boundary came from the API, with the percentage used |
| `inferred` | the 5-hour block was guessed from the gaps between messages |
| `rolling`  | a plain span back from now — 30d always, and 7d without a boundary |

Only `inferred` carries uncertainty. A `rolling` window is exact by definition.

A run that falls back says why on stderr, so a missing QUOTA column is never a
silent mystery:

```
$ ask-quota
window 5h: since 2026-08-14T21:58:07+07:00 (inferred)
...
ask-quota: Claude Code sign-in has expired; run claude once to renew it
```

## Limitation worth knowing

**Only transcripts on this machine are visible.** Sessions from another
machine, the web app, or cloud agents consume the same quota but leave no local
trace. What you see is a breakdown of local work, not necessarily of everything
the account consumed.

## License

MIT
