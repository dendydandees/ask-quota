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

Download the binary from the releases page, or build it:

```sh
go install github.com/dendy-fisiohome/ask-quota/cmd/ask-quota@latest
```

From a clone, `make install` also places the man page under `~/.local/share/man`.

No runtime, no dependencies: a single static binary, Go standard library only.
Linux is the supported target.

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

The 5-hour and 7-day windows mirror the ones the service enforces. If a quota
source is installed, boundaries come from it. If not, `ask-quota` infers the
current 5-hour block from the transcripts themselves: a block opens on a
message and closes five hours later, so the current one begins at the first
message after the previous closed. Against a live window that inference landed
within nine minutes.

The 30-day window has no upstream equivalent, so it is always the last 30 days
and never carries a QUOTA column.

## Limitation worth knowing

**Only transcripts on this machine are visible.** Sessions from another
machine, the web app, or cloud agents consume the same quota but leave no local
trace. What you see is a breakdown of local work, not necessarily of everything
the account consumed.

## License

MIT
