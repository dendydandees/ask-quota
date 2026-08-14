// Package window resolves a named reporting window to an absolute start time.
package window

import (
	"fmt"
	"sort"
	"time"
)

// The window names accepted on the command line.
const (
	Session = "5h"
	Week    = "week"
	Month   = "30d"
)

// Kinds lists the valid window names, in the order they are shown to users.
var Kinds = []string{Session, Week, Month}

const sessionLen = 5 * time.Hour

// Resolve returns the instant a window starts.
//
// reset is the official end of the current quota window, or the zero time when
// no quota source is available. times are the timestamps of recent activity,
// used only to infer a session block when reset is absent.
//
// The 30-day window has no upstream equivalent, so it ignores reset entirely.
func Resolve(kind string, now, reset time.Time, times []time.Time) (time.Time, error) {
	switch kind {
	case Session:
		if !reset.IsZero() {
			return reset.Add(-sessionLen), nil
		}
		return inferBlockStart(times, now), nil
	case Week:
		if !reset.IsZero() {
			return reset.AddDate(0, 0, -7), nil
		}
		return now.AddDate(0, 0, -7), nil
	case Month:
		return now.AddDate(0, 0, -30), nil
	default:
		return time.Time{}, fmt.Errorf("unknown window %q: want one of %v", kind, Kinds)
	}
}

// Lookback returns how far back to scan for a window. For a session it reaches
// well past the window itself: the block start is defined by an idle gap, and
// the gap can only be seen from activity that precedes it.
func Lookback(kind string, now time.Time) time.Time {
	if kind == Session {
		return now.Add(-24 * time.Hour)
	}
	start, err := Resolve(kind, now, time.Time{}, nil)
	if err != nil {
		return now.Add(-24 * time.Hour)
	}
	return start
}

// inferBlockStart replays activity forward to find the current block.
//
// A block opens on a message and closes a fixed five hours later, whether or
// not work continues; the next message after it closes opens a new block.
// Walking backwards from the newest message instead would report the start of
// an unbroken run of work, which can be many hours before the block that is
// actually open.
func inferBlockStart(times []time.Time, now time.Time) time.Time {
	if len(times) == 0 {
		return now.Add(-sessionLen)
	}

	sorted := make([]time.Time, len(times))
	copy(sorted, times)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })

	start := sorted[0]
	for _, t := range sorted[1:] {
		if !t.Before(start.Add(sessionLen)) {
			start = t
		}
	}

	// The last block to open may itself have closed since. There is then no open
	// block at all — the next message will start one — so the current window is
	// empty. Returning the rolling floor instead would sweep in the tail of the
	// closed block, which is work that no longer counts against the live quota.
	if !now.Before(start.Add(sessionLen)) {
		return now
	}
	return start
}
