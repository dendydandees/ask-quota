package quota

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dendydandees/ask-quota/internal/window"
)

// refuseAnyRequest fails the test if the network is touched at all.
func refuseAnyRequest(t *testing.T) {
	t.Helper()
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("a request was sent when none should have been")
	})
}

func TestLookupReadsAWorkingWindow(t *testing.T) {
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"sk-tok"}}`)
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(liveLimits))
	})

	got, reason := Lookup(window.Session)
	if reason != "" {
		t.Fatalf("reason = %q, want the window found", reason)
	}
	if got.PercentUsed != 7 || got.ResetsAt.IsZero() {
		t.Errorf("Lookup = %+v, want 7%% used and a real reset", got)
	}
}

// There is no 30-day window upstream, so nothing should go out on the wire for
// one — not a slow path, a bug.
func TestLookupNeverAsksAboutTheMonthlyWindow(t *testing.T) {
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"sk-tok"}}`)
	refuseAnyRequest(t)

	if _, reason := Lookup(window.Month); reason == "" {
		t.Error("30d was reported as official")
	}
}

// The tool made no network calls at all before this; the switch gives that
// back to anyone who wants it.
func TestLookupHonoursTheOfflineSwitch(t *testing.T) {
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"sk-tok"}}`)
	refuseAnyRequest(t)
	t.Setenv(offlineEnv, "1")

	_, reason := Lookup(window.Session)
	if !strings.Contains(reason, offlineEnv) {
		t.Errorf("reason = %q, want it to name the switch", reason)
	}
}

// Credentials are read before the request, so an unusable token costs no
// timeout and reports the one cause the user can actually fix.
func TestLookupStopsBeforeTheNetworkWithoutCredentials(t *testing.T) {
	writeCredentials(t, "")
	refuseAnyRequest(t)

	if _, reason := Lookup(window.Session); reason == "" {
		t.Error("a lookup without credentials was reported as official")
	}
}

// Whatever went wrong, the reason is shown to the user, so it must never carry
// the token that caused it.
func TestLookupKeepsTheTokenOutOfEveryReason(t *testing.T) {
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"sk-secret"}}`)

	for _, h := range []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) },
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(429) },
		func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("junk")) },
	} {
		serveUsage(t, h)
		if _, reason := Lookup(window.Session); strings.Contains(reason, "sk-secret") {
			t.Errorf("reason leaks the token: %q", reason)
		}
	}
}
