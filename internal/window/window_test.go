package window

import (
	"testing"
	"time"
)

func ts(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad test timestamp %q: %v", s, err)
	}
	return v
}

func TestResolveUsesOfficialResetWhenAvailable(t *testing.T) {
	now := ts(t, "2026-08-14T07:00:00Z")
	reset := ts(t, "2026-08-14T09:20:00Z")

	cases := []struct {
		kind string
		want string
	}{
		{Session, "2026-08-14T04:20:00Z"},
		{Week, "2026-08-07T09:20:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			got, err := Resolve(tc.kind, now, reset, nil)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if want := ts(t, tc.want); !got.Equal(want) {
				t.Errorf("Resolve(%s) = %v, want %v", tc.kind, got, want)
			}
		})
	}
}

// No 30-day window exists upstream, so an official reset must not leak into it.
func TestResolveMonthIgnoresOfficialReset(t *testing.T) {
	now := ts(t, "2026-08-14T07:00:00Z")
	reset := ts(t, "2026-08-14T09:20:00Z")

	got, err := Resolve(Month, now, reset, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := now.AddDate(0, 0, -30); !got.Equal(want) {
		t.Errorf("Resolve(30d) = %v, want %v", got, want)
	}
}

// Without an official reset, the 5-hour block starts at the first message
// after an idle gap longer than the window itself.
func TestResolveInfersBlockStartFromActivityGap(t *testing.T) {
	now := ts(t, "2026-08-14T07:00:00Z")
	times := []time.Time{
		ts(t, "2026-08-13T20:00:00Z"),
		ts(t, "2026-08-13T21:30:00Z"), // still the old block
		ts(t, "2026-08-14T04:28:45Z"), // >5h later: a new block starts here
		ts(t, "2026-08-14T05:10:00Z"),
		ts(t, "2026-08-14T06:59:00Z"),
	}

	got, err := Resolve(Session, now, time.Time{}, times)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := ts(t, "2026-08-14T04:28:45Z"); !got.Equal(want) {
		t.Errorf("inferred start = %v, want %v", got, want)
	}
}

// Recorded from a live window: official start 04:20:00Z, first local activity
// 04:28:45Z. The inference must stay close to the real boundary.
func TestInferredStartIsCloseToTheRealWindow(t *testing.T) {
	now := ts(t, "2026-08-14T07:29:00Z")
	official := ts(t, "2026-08-14T04:20:00Z")
	times := []time.Time{
		ts(t, "2026-08-13T22:10:03Z"),
		ts(t, "2026-08-14T04:28:45Z"),
		ts(t, "2026-08-14T06:00:00Z"),
	}

	got, err := Resolve(Session, now, time.Time{}, times)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if drift := got.Sub(official); drift < 0 || drift > 15*time.Minute {
		t.Errorf("inferred start drifts %v from the official start, want under 15m", drift)
	}
}

// A block is a fixed 5 hours: it closes on schedule and the next message opens
// a new one. Continuous activity does not extend it.
func TestResolveAdvancesBlockAfterFiveHoursOfContinuousActivity(t *testing.T) {
	now := ts(t, "2026-08-14T13:00:00Z")
	times := []time.Time{
		ts(t, "2026-08-14T04:00:00Z"), // opens a block, closing 09:00
		ts(t, "2026-08-14T06:00:00Z"), // inside it
		ts(t, "2026-08-14T09:30:00Z"), // past the close: opens the current block
		ts(t, "2026-08-14T12:00:00Z"), // inside the current block
	}

	got, err := Resolve(Session, now, time.Time{}, times)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := ts(t, "2026-08-14T09:30:00Z"); !got.Equal(want) {
		t.Errorf("got %v, want %v — the block must advance, not stretch", got, want)
	}
}

func TestResolveInfersOldestWhenActivityIsContinuous(t *testing.T) {
	now := ts(t, "2026-08-14T07:00:00Z")
	times := []time.Time{
		ts(t, "2026-08-14T05:00:00Z"),
		ts(t, "2026-08-14T06:00:00Z"),
	}

	got, err := Resolve(Session, now, time.Time{}, times)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := ts(t, "2026-08-14T05:00:00Z"); !got.Equal(want) {
		t.Errorf("got %v, want the oldest message %v", got, want)
	}
}

func TestResolveFallsBackToRollingWindowWithoutActivity(t *testing.T) {
	now := ts(t, "2026-08-14T07:00:00Z")

	cases := map[string]time.Duration{
		Session: 5 * time.Hour,
		Week:    7 * 24 * time.Hour,
	}
	for kind, back := range cases {
		t.Run(kind, func(t *testing.T) {
			got, err := Resolve(kind, now, time.Time{}, nil)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if want := now.Add(-back); !got.Equal(want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestResolveRejectsUnknownKind(t *testing.T) {
	if _, err := Resolve("9y", time.Now(), time.Time{}, nil); err == nil {
		t.Fatal("want an error for an unknown window")
	}
}

// Scanning must start early enough to see the gap that defines the block.
func TestLookbackReachesBeforeTheWindow(t *testing.T) {
	now := ts(t, "2026-08-14T07:00:00Z")

	if got, want := Lookback(Session, now), now.Add(-24*time.Hour); !got.Equal(want) {
		t.Errorf("Lookback(5h) = %v, want %v", got, want)
	}
	if got, want := Lookback(Week, now), now.Add(-7*24*time.Hour); !got.Equal(want) {
		t.Errorf("Lookback(week) = %v, want %v", got, want)
	}
	if got, want := Lookback(Month, now), now.AddDate(0, 0, -30); !got.Equal(want) {
		t.Errorf("Lookback(30d) = %v, want %v", got, want)
	}
}

// An unknown window still has to produce a usable scan floor: Lookback runs
// before the name is validated for reporting.
func TestLookbackFallsBackForUnknownKind(t *testing.T) {
	now := ts(t, "2026-08-14T07:00:00Z")

	if got, want := Lookback("9y", now), now.Add(-24*time.Hour); !got.Equal(want) {
		t.Errorf("Lookback(9y) = %v, want the 24h floor %v", got, want)
	}
}

// A block closes five hours after it opens, whether or not anyone is working.
// If the newest activity predates that close, the current block is empty — the
// honest answer is the rolling floor, not the last block that happened to open.
func TestResolveDoesNotReviveAClosedBlock(t *testing.T) {
	now := ts(t, "2026-08-14T20:00:00Z")
	times := []time.Time{
		ts(t, "2026-08-14T08:00:00Z"), // opened a block that closed at 13:00
		ts(t, "2026-08-14T09:00:00Z"),
	}

	got, err := Resolve(Session, now, time.Time{}, times)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := now.Add(-5 * time.Hour); !got.Equal(want) {
		t.Errorf("got %v, want %v — that block closed seven hours ago", got, want)
	}
}

// The boundary case: activity exactly at the edge of the open block still
// counts, so a block is not declared dead one instant early.
func TestResolveKeepsABlockThatIsStillOpen(t *testing.T) {
	now := ts(t, "2026-08-14T12:59:00Z")
	times := []time.Time{ts(t, "2026-08-14T08:00:00Z")}

	got, err := Resolve(Session, now, time.Time{}, times)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := ts(t, "2026-08-14T08:00:00Z"); !got.Equal(want) {
		t.Errorf("got %v, want %v — the block closes at 13:00, one minute from now", got, want)
	}
}
