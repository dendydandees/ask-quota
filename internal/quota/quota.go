// Package quota reads the official view of a quota window from the same API
// Claude Code itself authenticates against, using the token already on disk.
package quota

import (
	"os"
	"time"
)

// Official is the upstream view of a quota window: when it resets and how much
// of it has been consumed.
type Official struct {
	ResetsAt    time.Time
	PercentUsed float64
}

// offlineEnv skips the network entirely. ask-quota made no network calls at all
// before the official boundary moved in process, and that is a property some
// people will want back on demand.
const offlineEnv = "ASK_QUOTA_OFFLINE"

// Lookup returns the official view of a window, or a plain-English reason there
// is none. The reason is empty exactly when the window was found.
//
// Nothing on this path is an error. The report ranks sessions from local
// transcripts either way; the official boundary only lets those ranks be
// scaled to the real window, so its absence costs one column and never the run.
func Lookup(kind string) (Official, string) {
	if _, ok := limitGroup[kind]; !ok {
		return Official{}, noUpstreamWindow(kind)
	}
	if os.Getenv(offlineEnv) != "" {
		return Official{}, offlineEnv + " is set"
	}

	now := time.Now()
	creds, reason := readCredentials(now)
	if reason != "" {
		return Official{}, reason
	}
	return fetchUsage(creds, kind, now)
}
