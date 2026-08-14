package cli

// Usage is the --help text. It is the on-screen twin of man/ask-quota.1;
// change one and change the other.
const Usage = `ask-quota — see which tasks consumed your Claude Code quota.

USAGE
  ask-quota [window] [options]

WINDOWS
  5h       the current 5-hour block (default)
  week     the current 7-day window
  30d      the last 30 days

OPTIONS
  --window <name>     window to report on, same names as above
  --top <n|all>       how many rows to show (default 5)
  --group-by <mode>   "session" (default) or "project", which merges
                      every session that ran in the same directory
  --json              print rows as JSON for piping; no header, no colour
  --version           print the version and exit
  --help              print this text and exit

COLUMNS
  SHARE    this row's percentage of the window's total, fully measured
  QUOTA    SHARE scaled by the official percentage used, so it estimates
           points of the real quota window. A proportion, not a
           measurement, and only shown when an official figure is available

EXAMPLES
  ask-quota                        top 5 sessions of the current block
  ask-quota week --group-by project    which repos consumed this week
  ask-quota 30d --top all --json | jq  everything, machine-readable

LIMITATIONS
  Only transcripts on this machine are visible. Sessions from another
  machine, the web app, or cloud agents consume the same quota but leave
  no local trace, so they cannot appear here.
`
