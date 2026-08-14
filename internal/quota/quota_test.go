package quota

import (
	"testing"
	"time"

	"github.com/dendy-fisiohome/ask-quota/internal/window"
)

// The quota source is optional by design. Every way it can go wrong must read
// as "absent", never as an error the user has to deal with.
func TestLookupTreatsEveryFailureAsAbsent(t *testing.T) {
	original := quotaSource
	t.Cleanup(func() { quotaSource = original })

	cases := []struct {
		name    string
		command []string
	}{
		{"binary not installed", []string{"ask-quota-no-such-command-exists"}},
		{"source exits non-zero", []string{"false"}},
		{"source prints junk", []string{"echo", "not json"}},
		{"source prints an unrelated window", []string{"echo", `{"providers":[{"windows":[{"id":"model:fable","percentUsed":7}]}]}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			quotaSource = tc.command
			if _, ok := Lookup(window.Session); ok {
				t.Error("reported success, want a clean absence")
			}
		})
	}
}

func TestLookupReadsAWorkingSource(t *testing.T) {
	original := quotaSource
	t.Cleanup(func() { quotaSource = original })

	quotaSource = []string{"echo", sample}
	got, ok := Lookup(window.Session)
	if !ok {
		t.Fatal("want the window to be found")
	}
	if got.PercentUsed != 51 {
		t.Errorf("PercentUsed = %v, want 51", got.PercentUsed)
	}
}

// There is no 30-day window upstream, so the source is never even consulted.
func TestLookupSkipsTheSourceForMonth(t *testing.T) {
	original := quotaSource
	t.Cleanup(func() { quotaSource = original })

	quotaSource = []string{"ask-quota-this-would-fail-if-run"}
	if _, ok := Lookup(window.Month); ok {
		t.Error("want no official window for 30d")
	}
}

const sample = `{"providers":[{"provider":"claude","windows":[
  {"id":"five_hour","percentUsed":51,"resetsAt":"2026-08-14T09:20:00.388297+00:00"},
  {"id":"seven_day","percentUsed":57,"resetsAt":"2026-08-16T05:59:59.906805+00:00"},
  {"id":"model:fable","percentUsed":7,"resetsAt":"2026-08-16T05:59:59.907168+00:00"}]}]}`

func TestParseReadsTheMatchingWindow(t *testing.T) {
	got, ok := parse([]byte(sample), window.Session)
	if !ok {
		t.Fatal("want the five_hour window to be found")
	}
	if got.PercentUsed != 51 {
		t.Errorf("PercentUsed = %v, want 51", got.PercentUsed)
	}
	if got.ResetsAt.IsZero() {
		t.Error("ResetsAt was not parsed")
	}

	week, ok := parse([]byte(sample), window.Week)
	if !ok || week.PercentUsed != 57 {
		t.Errorf("week = %+v, ok = %v, want 57", week, ok)
	}
}

// No 30-day window exists upstream, so there is nothing to match.
func TestParseHasNoMonthWindow(t *testing.T) {
	if _, ok := parse([]byte(sample), window.Month); ok {
		t.Error("want no official window for 30d")
	}
}

func TestParseToleratesJunk(t *testing.T) {
	for _, in := range []string{"", "not json", `{"providers":[]}`, `{"providers":[{"windows":[]}]}`} {
		if _, ok := parse([]byte(in), window.Session); ok {
			t.Errorf("parse(%q) reported success, want a clean miss", in)
		}
	}
}

// A source that forks leaves a descendant holding the stdout pipe, so killing
// the child is not enough — Wait would block forever and take the whole program
// with it. The contract is that a slow source is simply absent.
func TestLookupDoesNotHangOnAForkingSource(t *testing.T) {
	original := quotaSource
	t.Cleanup(func() { quotaSource = original })

	lookupTimeout = 200 * time.Millisecond
	t.Cleanup(func() { lookupTimeout = 8 * time.Second })

	// /bin/sh forks rather than execs, so `sleep` survives the kill.
	quotaSource = []string{"/bin/sh", "-c", "sleep 60 & exit 0"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, ok := Lookup("5h"); ok {
			t.Error("a forking source reported success")
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Lookup hung: the timeout does not cover a descendant holding stdout")
	}
}

// A runaway source must not be read into memory without a ceiling.
func TestLookupBoundsTheOutputItReads(t *testing.T) {
	original := quotaSource
	t.Cleanup(func() { quotaSource = original })

	lookupTimeout = 200 * time.Millisecond
	t.Cleanup(func() { lookupTimeout = 8 * time.Second })

	quotaSource = []string{"/bin/sh", "-c", "exec yes '{\"providers\":[]}'"}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, ok := Lookup("5h"); ok {
			t.Error("an endless source reported success")
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Lookup did not stop reading an endless source")
	}
}
