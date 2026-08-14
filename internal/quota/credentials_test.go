package quota

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCredentials points the lookup at a temporary config directory holding
// the given file body, and returns the moment tokens in it are measured
// against.
func writeCredentials(t *testing.T, body string) time.Time {
	t.Helper()

	dir := t.TempDir()
	if body != "" {
		path := filepath.Join(dir, ".credentials.json")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	return time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
}

// The token sits in one of three places depending on how the account was set
// up, and all three are in the wild. Reading only the nested one would leave
// some users silently in guess-mode with a perfectly good token on disk.
func TestCredentialsAcceptEveryTokenLocation(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"nested", `{"claudeAiOauth":{"accessToken":"sk-tok","subscriptionType":"max"}}`},
		{"top level camel", `{"accessToken":"sk-tok"}`},
		{"top level snake", `{"access_token":"sk-tok"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := writeCredentials(t, tc.body)

			creds, reason := readCredentials(now)
			if reason != "" {
				t.Fatalf("reason = %q, want the token accepted", reason)
			}
			if creds.accessToken != "sk-tok" {
				t.Errorf("accessToken = %q, want sk-tok", creds.accessToken)
			}
		})
	}
}

// An expired token is worth a distinct answer rather than a generic failure:
// it is the one cause the user can fix, and sending the request anyway buys a
// guaranteed 401 at the cost of the whole timeout budget.
func TestCredentialsRejectAnExpiredToken(t *testing.T) {
	now := writeCredentials(t,
		`{"claudeAiOauth":{"accessToken":"sk-tok","expiresAt":1000000000000}}`)

	creds, reason := readCredentials(now)
	if reason == "" {
		t.Fatal("an expired token was accepted")
	}
	if creds.accessToken != "" {
		t.Error("an expired token was still handed back")
	}
	if !strings.Contains(reason, "expired") {
		t.Errorf("reason = %q, want it to name the expiry", reason)
	}
}

// A token expiring at this very instant is expired: !After(now) rather than
// Before(now), so the boundary case does not slip through as live.
func TestCredentialsRejectATokenExpiringExactlyNow(t *testing.T) {
	now := writeCredentials(t,
		`{"claudeAiOauth":{"accessToken":"sk-tok","expiresAt":1786000000000}}`)
	exactly := time.UnixMilli(1786000000000)
	_ = now

	if _, reason := readCredentials(exactly); reason == "" {
		t.Error("a token expiring exactly now was accepted")
	}
}

// An empty nested object must not veto a good top-level token: nested wins only
// when it actually carries one.
func TestCredentialsFallThroughAnEmptyNestedObject(t *testing.T) {
	now := writeCredentials(t, `{"claudeAiOauth":{},"accessToken":"sk-tok"}`)

	creds, reason := readCredentials(now)
	if reason != "" || creds.accessToken != "sk-tok" {
		t.Errorf("got %q, %q; want the top-level token used", creds.accessToken, reason)
	}
}

// The millisecond unit is pinned by the expiry test above: its value reads as
// 2001 in milliseconds but as the year 33658 in seconds, so a seconds-based
// implementation would call that token live and fail there. This case guards
// the other direction — a token with time left must not be rejected.
func TestCredentialsAcceptATokenWithTimeLeft(t *testing.T) {
	now := writeCredentials(t,
		`{"claudeAiOauth":{"accessToken":"sk-tok","expiresAt":1790000000000}}`)

	if _, reason := readCredentials(now); reason != "" {
		t.Errorf("reason = %q, want a token expiring after now to be live", reason)
	}
}

// Absence is not expiry: a file without the field must not be read as a token
// that expired at the epoch.
func TestCredentialsTreatAMissingExpiryAsLive(t *testing.T) {
	now := writeCredentials(t, `{"claudeAiOauth":{"accessToken":"sk-tok"}}`)

	if _, reason := readCredentials(now); reason != "" {
		t.Errorf("reason = %q, want no expiry to mean no expiry", reason)
	}
}

func TestCredentialsDegradeOnUnusableFiles(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"absent", ""},
		{"malformed", `{"claudeAiOauth":`},
		{"empty object", `{}`},
		{"token not a string", `{"accessToken":42}`},
		// Nested present but empty, with nothing at the top level either.
		{"nested holds no token", `{"claudeAiOauth":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := writeCredentials(t, tc.body)

			creds, reason := readCredentials(now)
			if reason == "" {
				t.Error("an unusable credentials file was accepted")
			}
			if creds.accessToken != "" {
				t.Errorf("accessToken = %q, want none", creds.accessToken)
			}
		})
	}
}

// The reason is printed to the user. A token that leaks into it would end up
// in terminal scrollback, screenshots and pasted bug reports.
func TestCredentialsKeepTheTokenOutOfTheReason(t *testing.T) {
	for _, body := range []string{
		`{"claudeAiOauth":{"accessToken":"sk-secret","expiresAt":1000000000000}}`,
		`{"accessToken":"sk-secret","expiresAt":1}`,
	} {
		now := writeCredentials(t, body)

		if _, reason := readCredentials(now); strings.Contains(reason, "sk-secret") {
			t.Errorf("reason leaks the token: %q", reason)
		}
	}
}

// Without the override the file is found under HOME, which is what every real
// run depends on.
func TestCredentialsFallBackToHome(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".claude", ".credentials.json")
	if err := os.WriteFile(path, []byte(`{"accessToken":"sk-tok"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", home)

	creds, reason := readCredentials(time.Now())
	if reason != "" || creds.accessToken != "sk-tok" {
		t.Errorf("readCredentials = %q, %q; want the token from HOME", creds.accessToken, reason)
	}
}

// The nested layout is the one Claude Code writes, so it must win outright. A
// stray top-level key of the wrong type used to veto a nested token that had
// parsed perfectly well, degrading a run that had everything it needed.
func TestCredentialsPreferNestedOverAMalformedTopLevel(t *testing.T) {
	now := writeCredentials(t,
		`{"claudeAiOauth":{"accessToken":"sk-good","expiresAt":1790000000000},
		  "expiresAt":"not a number"}`)

	creds, reason := readCredentials(now)
	if reason != "" {
		t.Fatalf("reason = %q, want the nested token used", reason)
	}
	if creds.accessToken != "sk-good" {
		t.Errorf("accessToken = %q, want sk-good", creds.accessToken)
	}
}
