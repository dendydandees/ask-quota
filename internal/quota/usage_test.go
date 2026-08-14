package quota

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dendydandees/ask-quota/internal/window"
)

// serveUsage stands in for the usage endpoint and points the fetcher at it.
// Nothing in this file ever reaches the real API: a live call in the suite
// would need a real token and would fail on any machine without one.
//
// It also gives the test its own cache directory. Without that the first test
// to run populates the real one under $HOME and every later test reads that
// payload instead of the one it served.
func serveUsage(t *testing.T, h http.HandlerFunc) {
	t.Helper()

	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	// The real path is kept so the request contract — method and path, not just
	// headers — can be asserted against the stub.
	usageURL = srv.URL + "/api/oauth/usage"
	t.Cleanup(func() { usageURL = defaultUsageURL })

	t.Setenv("XDG_CACHE_HOME", t.TempDir())
}

// testNow is a fixed instant so cache freshness is never a matter of timing.
var testNow = time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

const liveLimits = `{"limits":[
  {"group":"session","percent":7,"resets_at":"2026-08-14T19:50:00.233892+00:00"},
  {"group":"weekly","percent":61,"resets_at":"2026-08-16T06:00:00.233910+00:00"},
  {"scope":{"model":{"id":"fable","display_name":"Fable"}},
   "percent":7,"resets_at":"2026-08-16T06:00:00.234112+00:00"}
]}`

// The shape captured from a live account on 2026-08-14. Six fractional digits
// and a numeric offset rather than Z, which is what actually comes back.
func TestUsageParsesTheLiveShape(t *testing.T) {
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer sk-tok" {
			t.Errorf("authorization = %q, want the bearer token", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != oauthBeta {
			t.Errorf("anthropic-beta = %q, want %q", got, oauthBeta)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET: this request must not have a side effect", r.Method)
		}
		if r.URL.Path != "/api/oauth/usage" {
			t.Errorf("path = %q, want the usage endpoint", r.URL.Path)
		}
		w.Write([]byte(liveLimits))
	})

	for _, tc := range []struct {
		kind    string
		percent float64
		resets  string
	}{
		{window.Session, 7, "2026-08-14T19:50:00.233892+00:00"},
		{window.Week, 61, "2026-08-16T06:00:00.233910+00:00"},
	} {
		got, reason := fetchUsage(credentials{accessToken: "sk-tok"}, tc.kind, testNow)
		if reason != "" {
			t.Fatalf("%s: %s, in a payload that contains it", tc.kind, reason)
		}
		want, err := time.Parse(time.RFC3339, tc.resets)
		if err != nil {
			t.Fatal(err)
		}
		if !got.ResetsAt.Equal(want) {
			t.Errorf("%s ResetsAt = %v, want %v", tc.kind, got.ResetsAt, want)
		}
		if got.PercentUsed != tc.percent {
			t.Errorf("%s PercentUsed = %v, want %v", tc.kind, got.PercentUsed, tc.percent)
		}
	}
}

// A scoped entry is a weekly window for one model or surface, not the
// account's. Mistaking it for one would report the wrong reset and the wrong
// percentage while looking entirely plausible.
func TestUsageIgnoresScopedLimits(t *testing.T) {
	for _, scope := range []string{
		`{"model":{"id":"fable","display_name":"Fable"}}`,
		`{"model":{"id":null,"display_name":"Fable"},"surface":null}`, // the live shape
		`{"model":null,"surface":"cowork"}`,
	} {
		serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"limits":[{"group":"weekly","scope":` + scope + `,
			  "percent":99,"resets_at":"2026-08-16T06:00:00Z"}]}`))
		})

		if got, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Week, testNow); reason == "" {
			t.Errorf("a scoped window was reported as the weekly window: %+v", got)
		}
	}
}

// The account's own entries carry a null scope, and both they and the scoped
// ones share a group. Picking the first match in the array would be luck.
func TestUsagePicksTheUnscopedWindowWhateverTheOrder(t *testing.T) {
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"limits":[
		  {"kind":"weekly_scoped","group":"weekly","scope":{"model":{"display_name":"Fable"}},
		   "percent":7,"resets_at":"2026-08-16T06:00:00Z"},
		  {"kind":"weekly_all","group":"weekly","scope":null,
		   "percent":62,"resets_at":"2026-08-16T06:00:00Z"}]}`))
	})

	got, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Week, testNow)
	if reason != "" || got.PercentUsed != 62 {
		t.Errorf("got %+v, %q; want the unscoped 62%% even though it is second", got, reason)
	}
}

// The older payload uses different keys for the same numbers. Accounts still
// on it must not silently fall back to guessing.
func TestUsageFallsBackToTheLegacyShape(t *testing.T) {
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"five_hour":{"utilization":12,"resets_at":"2026-08-14T19:50:00Z"},
		  "seven_day":{"utilization":34,"reset_at":"2026-08-16T06:00:00Z"}}`))
	})

	got, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, testNow)
	if reason != "" || got.PercentUsed != 12 {
		t.Errorf("five_hour = %+v, %q; want 12%% used", got, reason)
	}
	// seven_day carries reset_at rather than resets_at in this shape.
	if got, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Week, testNow); reason != "" || got.PercentUsed != 34 {
		t.Errorf("seven_day = %+v, %q; want 34%% used", got, reason)
	}
}

// Both shapes in one payload is what a migration looks like from the outside.
// limits is the vendor's authoritative list, so it wins.
func TestUsagePrefersLimitsOverTheLegacyShape(t *testing.T) {
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"limits":[{"group":"session","percent":7,
		    "resets_at":"2026-08-14T19:50:00Z"}],
		  "five_hour":{"utilization":99,"resets_at":"2020-01-01T00:00:00Z"}}`))
	})

	got, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, testNow)
	if reason != "" || got.PercentUsed != 7 {
		t.Errorf("PercentUsed = %+v, %q; want limits to win with 7", got, reason)
	}
}

func TestUsageDegradesOnEveryFailure(t *testing.T) {
	seen := map[string]string{}
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
	}{
		{"unauthorized", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) }},
		{"forbidden", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }},
		{"rate limited", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) }},
		{"server error", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }},
		{"malformed", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"limits":`)) }},
		{"empty", func(w http.ResponseWriter, r *http.Request) {}},
		{"html", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>nope</html>")) }},
		{"no window", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"limits":[]}`)) }},
		{"reset unparseable", func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"limits":[{"group":"session","percent":7,"resets_at":"soon"}]}`))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serveUsage(t, tc.h)

			got, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, testNow)
			if reason == "" {
				t.Fatalf("a failed lookup was reported as official: %+v", got)
			}
			seen[tc.name] = reason
		})
	}

	// A sign-in that needs renewing, a rate limit and a broken payload have
	// different fixes. Collapsing them into one message would send the user
	// looking in the wrong place.
	for _, pair := range [][2]string{
		{"unauthorized", "rate limited"},
		{"unauthorized", "server error"},
		{"rate limited", "server error"},
		{"malformed", "no window"},
	} {
		if seen[pair[0]] != "" && seen[pair[0]] == seen[pair[1]] {
			t.Errorf("%s and %s share the reason %q", pair[0], pair[1], seen[pair[0]])
		}
	}
}

// A source that never stops writing must not be able to exhaust memory before
// the timeout fires.
//
// The payload is valid and padded past the cap, not 4 MiB of junk: junk is
// rejected by the parser whether or not the body is bounded, so a junk fixture
// passes with the cap deleted and proves nothing about it.
func TestUsageBoundsTheResponseBody(t *testing.T) {
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"limits":[{"group":"session","percent":7,` +
			`"resets_at":"2026-08-14T19:50:00Z","pad":"` +
			strings.Repeat("a", 2*maxBody) + `"}]}`))
	})

	_, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, testNow)
	if !strings.Contains(reason, "more than we will read") {
		t.Errorf("reason = %q, want the size named — an unbounded read would have parsed this", reason)
	}
}

// run blocks on the lookup, so a stalled endpoint is the one failure that costs
// the whole run rather than a column. The comment on usageTimeout justifies its
// value; this pins its existence.
func TestUsageGivesUpRatherThanHanging(t *testing.T) {
	original := usageClient.Timeout
	usageClient.Timeout = 50 * time.Millisecond
	t.Cleanup(func() { usageClient.Timeout = original })

	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	})

	done := make(chan string, 1)
	go func() {
		_, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, testNow)
		done <- reason
	}()

	select {
	case reason := <-done:
		if reason == "" {
			t.Error("a timed-out lookup was reported as official")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the lookup never returned; the CLI would hang with no output")
	}
}

// The token is sent to whatever this constant says. Every other test replaces
// it with an httptest server, so a downgraded scheme or a typo'd path would be
// invisible — and a bearer token over plain http is the worst outcome here.
func TestUsageTargetsTheOfficialEndpointOverTLS(t *testing.T) {
	if defaultUsageURL != "https://api.anthropic.com/api/oauth/usage" {
		t.Errorf("defaultUsageURL = %q; this is where the access token goes", defaultUsageURL)
	}
}

// The token authenticates every request, so a redirect to another host would
// hand it to whoever controls that host.
func TestUsageRefusesToFollowRedirectsOffHost(t *testing.T) {
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "" {
			t.Error("the token was sent to the redirect target")
		}
		w.Write([]byte(liveLimits))
	}))
	t.Cleanup(elsewhere.Close)

	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL, http.StatusFound)
	})

	if _, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, testNow); reason == "" {
		t.Error("a redirected lookup was accepted")
	}
}

// 30d has no upstream window, so asking about it is a bug, not a slow path.
func TestUsageNeverAsksAboutTheMonthlyWindow(t *testing.T) {
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("30d reached the network")
	})

	if _, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Month, testNow); reason == "" {
		t.Error("30d was reported as official")
	}
}

// The percentage is multiplied into every QUOTA cell, so a payload reporting
// 5000 would print a confidently wrong column rather than an obviously broken
// one. Upstream clamps; inlining the lookup dropped that by accident.
func TestUsageKeepsThePercentageInRange(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want float64
	}{
		{"5000", 100},
		{"-3", 0},
		{"101", 100}, // extra usage can push past the limit; the bar still fills once
		{"61", 61},
	} {
		serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"limits":[{"group":"session","percent":` + tc.raw +
				`,"resets_at":"2026-08-14T19:50:00Z"}]}`))
		})

		got, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, testNow)
		if reason != "" {
			t.Fatalf("percent %s: %s", tc.raw, reason)
		}
		if got.PercentUsed != tc.want {
			t.Errorf("percent %s became %v, want %v", tc.raw, got.PercentUsed, tc.want)
		}
	}
}

// limits is the vendor's authoritative list, so the choice between it and the
// older keys belongs to the payload, not to each window in turn. Deciding it
// per window lets one renamed group fall through to a stale legacy figure that
// still comes back with no reason — and so gets printed as "official".
func TestUsageNeverFallsBackWhenLimitsArePresent(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{
			// weekly_all is the kind this account's live payload actually uses;
			// upstream renaming group to match would land here.
			"group renamed upstream",
			`{"limits":[{"group":"session","percent":7,"resets_at":"2026-08-14T19:50:00Z"},
			  {"group":"weekly_all","percent":62,"resets_at":"2026-08-16T06:00:00Z"}],
			  "seven_day":{"utilization":9,"reset_at":"2026-08-16T06:00:00Z"}}`,
		},
		{
			// Otherwise the legacy key quietly undoes the scoped-entry skip.
			"only a scoped weekly entry",
			`{"limits":[{"group":"weekly","scope":{"model":{"id":"fable"}},
			   "percent":99,"resets_at":"2026-08-16T06:00:00Z"}],
			  "seven_day":{"utilization":5,"reset_at":"2026-08-16T06:00:00Z"}}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(tc.body))
			})

			got, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Week, testNow)
			if reason == "" {
				t.Errorf("a legacy figure was reported as official: %+v", got)
			}
		})
	}
}

// A boundary that has already passed is not a boundary. Rejecting it catches a
// stale payload independently of where in the payload it came from.
func TestUsageRejectsAResetAlreadyPast(t *testing.T) {
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"limits":[{"group":"session","percent":7,
		  "resets_at":"2020-01-01T00:00:00Z"}]}`))
	})

	if got, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, testNow); reason == "" {
		t.Errorf("a reset from 2020 was accepted: %+v", got)
	}
}

// The older keys are still the answer for a payload that never grew a limits
// array. Making limits authoritative must not turn that into a guess.
func TestUsageStillReadsAPayloadWithNoLimitsArray(t *testing.T) {
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"five_hour":{"utilization":12,"resets_at":"2026-08-14T19:50:00Z"}}`))
	})

	got, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, testNow)
	if reason != "" || got.PercentUsed != 12 {
		t.Errorf("got %+v, %q; want 12%% from five_hour", got, reason)
	}
}

// The golden is the real payload with volatile values scrubbed, recorded by
// `make live-update`. Every other test in this file feeds parseUsage a fixture
// written by hand to match what the parser expects — which is circular. This
// one feeds it what the endpoint actually sends, all seventeen keys of it.
//
// It also pins the assumption the parser leans on hardest: the account's own
// entries carry a null scope while a scoped one carries an object, and both
// share a group. Nothing upstream promises that.
func TestParseReadsTheRecordedLivePayload(t *testing.T) {
	golden, err := os.ReadFile("testdata/usage-live.json")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ kind, group string }{
		{window.Session, "session"},
		{window.Week, "weekly"},
	} {
		got, reason := parseUsage(golden, tc.kind, testNow)
		if reason != "" {
			t.Errorf("%s: %s", tc.kind, reason)
			continue
		}
		if got.PercentUsed != 50 {
			t.Errorf("%s PercentUsed = %v, want the scrubbed 50", tc.kind, got.PercentUsed)
		}
		if got.ResetsAt.IsZero() {
			t.Errorf("%s reset was not parsed", tc.kind)
		}
	}

	// The weekly group holds an unscoped entry and a Fable-scoped one. Reading
	// the scoped one would be a plausible wrong number, so prove which won.
	var payload usagePayload
	if err := json.Unmarshal(golden, &payload); err != nil {
		t.Fatal(err)
	}
	var weeklyUnscoped, weeklyScoped int
	for _, l := range payload.Limits {
		if l.Group != "weekly" {
			continue
		}
		if l.Scope == nil {
			weeklyUnscoped++
		} else {
			weeklyScoped++
		}
	}
	if weeklyUnscoped != 1 || weeklyScoped != 1 {
		t.Errorf("golden holds %d unscoped and %d scoped weekly entries; want one of each — "+
			"the ambiguity this parser resolves is no longer recorded", weeklyUnscoped, weeklyScoped)
	}
}
