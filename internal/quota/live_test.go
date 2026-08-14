//go:build live

// This file is excluded from every ordinary run. It makes real requests with a
// real token, so it needs a signed-in machine and cannot work in CI.
//
//	go test -tags=live ./internal/quota/
//
// It is the only check that notices upstream changing the payload. Everything
// else in this package tests against a shape captured on 2026-08-14, and a
// captured shape stays green forever no matter what the endpoint starts
// returning. Run it when a report looks wrong, and before a release.
package quota

import (
	"bytes"
	"encoding/json"
	"flag"
	"math"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/dendydandees/ask-quota/internal/window"
)

// TestLiveMatchesQuotaAxi is deliberately one test making one request per
// window. The endpoint rate limits, and a file of small live tests trips it
// and then reports the limit rather than the thing it meant to check.
//
// A silent divergence from quota-axi is the failure this whole change risks:
// both numbers look plausible, and only one of them is right.
const (
	goldenPath = "testdata/usage-live.json"
	// Fixed and comfortably after the dates the untagged golden test reads it
	// with, so a scrubbed payload still parses as a live window.
	goldenReset = "2026-08-16T06:00:00.000000+00:00"
)

func TestLiveMatchesQuotaAxi(t *testing.T) {
	liveOnly(t)

	out, err := exec.Command("quota-axi", "--provider", "claude", "--json").Output()
	if err != nil {
		t.Skip("quota-axi is not installed; nothing to compare against")
	}

	var payload struct {
		Providers []struct {
			Windows []struct {
				ID          string  `json:"id"`
				PercentUsed float64 `json:"percentUsed"`
				ResetsAt    string  `json:"resetsAt"`
			} `json:"windows"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("quota-axi output is not the shape this test expects: %v", err)
	}

	for kind, id := range map[string]string{window.Session: "five_hour", window.Week: "seven_day"} {
		got, reason := Lookup(kind)
		if reason != "" {
			t.Fatalf("%s: %s", kind, reason)
		}

		var found bool
		for _, p := range payload.Providers {
			for _, w := range p.Windows {
				if w.ID != id {
					continue
				}
				found = true

				// The two calls are seconds apart and the percentage moves as
				// work continues, so exact equality would be flaky. A point of
				// drift is noise; ten is a different window.
				if math.Abs(got.PercentUsed-w.PercentUsed) > 1 {
					t.Errorf("%s PercentUsed = %v, quota-axi says %v",
						kind, got.PercentUsed, w.PercentUsed)
				}

				// resets_at is computed per request rather than being a fixed
				// instant: two calls a second apart came back 19ms and 454ms
				// apart for the 5h and 7d windows. Sub-second jitter is just
				// the endpoint answering twice; a genuinely different window
				// would be hours out.
				want, err := time.Parse(time.RFC3339, w.ResetsAt)
				if err != nil {
					t.Fatal(err)
				}
				if drift := got.ResetsAt.Sub(want); drift.Abs() > 5*time.Second {
					t.Errorf("%s ResetsAt = %v, quota-axi says %v (%v apart)",
						kind, got.ResetsAt, want, drift)
				}
			}
		}
		if !found {
			t.Errorf("quota-axi reported no %s window to compare against", id)
		}
	}
}

// liveOnly makes a live test live. Lookup reads the cache before the network,
// so without this a run within five minutes of any ordinary ask-quota
// invocation validates a stored payload — meaning the one test meant to notice
// an upstream shape change can pass against the shape from before it changed.
func liveOnly(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("ASK_QUOTA_OFFLINE", "")
}

// The shape assertions need no second implementation to compare against, so
// they must not sit behind the quota-axi skip. Since quota-axi was uninstalled,
// `go test -tags=live` reported ok while asserting nothing at all.
func TestLiveShapeStillParses(t *testing.T) {
	liveOnly(t)

	for _, kind := range []string{window.Session, window.Week} {
		got, reason := Lookup(kind)
		if reason != "" {
			t.Fatalf("%s: %s (sign in with claude, or the payload changed)", kind, reason)
		}
		if !got.ResetsAt.After(time.Now()) {
			t.Errorf("%s resets at %v, which has already passed", kind, got.ResetsAt)
		}
		if got.PercentUsed < 0 || got.PercentUsed > 100 {
			t.Errorf("%s PercentUsed = %v, want a percentage", kind, got.PercentUsed)
		}
	}
}

// updateGolden rewrites testdata/usage-live.json from the real endpoint:
//
//	go test -tags=live ./internal/quota/ -run TestLiveGolden -update
//
// The point is the diff. quota-axi was a second implementation to disagree
// with; nothing replaces that, and nothing should. What a golden gives instead
// is a shape change showing up as red lines in a file review before a release,
// with no second vendor's CLI involved.
var updateGolden = flag.Bool("update", false, "rewrite the golden payload from the live endpoint")

func TestLiveGoldenIsCurrent(t *testing.T) {
	liveOnly(t)

	creds, reason := readCredentials(time.Now())
	if reason != "" {
		t.Skipf("%s", reason)
	}
	// Straight to the endpoint: Lookup returns only the parsed window, and the
	// whole payload is what is being recorded.
	if _, reason := fetchUsage(creds, window.Session, time.Now()); reason != "" {
		t.Fatalf("live fetch: %s", reason)
	}
	stored, err := readAtMost(cachePath(), maxBody)
	if err != nil {
		t.Fatal(err)
	}
	var entry cacheEntry
	if err := json.Unmarshal(stored, &entry); err != nil {
		t.Fatal(err)
	}

	fresh, err := scrub(entry.Body)
	if err != nil {
		t.Fatal(err)
	}

	if *updateGolden {
		if err := os.WriteFile(goldenPath, fresh, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("%v — run with -update to create it", err)
	}
	if !bytes.Equal(bytes.TrimSpace(golden), bytes.TrimSpace(fresh)) {
		t.Errorf("the live payload no longer matches %s.\n"+
			"Re-run with -update and review the diff: a new window, a renamed\n"+
			"group or a moved key is exactly what this is here to surface.", goldenPath)
	}
}

// scrub replaces every volatile value with a fixed one, so the golden records
// the shape and nothing about the account. Percentages and reset times move on
// every request; keeping them would make the file churn and say nothing.
func scrub(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, inner := range t {
				switch k {
				case "percent", "utilization":
					if inner != nil {
						t[k] = 50.0
					}
				case "resets_at", "reset_at":
					if inner != nil {
						t[k] = goldenReset
					}
				default:
					walk(inner)
				}
			}
		case []any:
			for _, inner := range t {
				walk(inner)
			}
		}
	}
	walk(payload)
	return json.MarshalIndent(payload, "", "  ")
}
