package quota

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dendydandees/ask-quota/internal/window"
)

// countingUsage serves the live payload and counts how many requests actually
// reached the network.
func countingUsage(t *testing.T) *int {
	t.Helper()

	calls := 0
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(liveLimits))
	})
	return &calls
}

// The endpoint rate limits quickly, and without this every repeated run would
// eventually degrade to a guess — the exact failure the in-process lookup was
// meant to remove.
func TestCacheServesRepeatedLookupsWithoutARequest(t *testing.T) {
	calls := countingUsage(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		if _, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, now); reason != "" {
			t.Fatalf("lookup %d: %s", i, reason)
		}
	}
	if *calls != 1 {
		t.Errorf("sent %d requests, want 1 with the rest served from cache", *calls)
	}
}

// One GET returns every window, so asking about the week after the session
// must not cost a second request.
func TestCacheAnswersEveryWindowFromOnePayload(t *testing.T) {
	calls := countingUsage(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	session, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, now)
	if reason != "" {
		t.Fatal(reason)
	}
	week, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Week, now)
	if reason != "" {
		t.Fatal(reason)
	}

	if *calls != 1 {
		t.Errorf("sent %d requests, want one payload to answer both", *calls)
	}
	if session.PercentUsed != 7 || week.PercentUsed != 61 {
		t.Errorf("got %v and %v, want the two windows read from the same payload",
			session.PercentUsed, week.PercentUsed)
	}
}

// The cache exists to stop a burst, not to freeze the number. Probed against
// literal durations rather than cacheTTL itself: measuring the constant with
// the constant would stay green if it grew to five hours, which is exactly the
// change its own comment forbids.
func TestCacheExpires(t *testing.T) {
	if cacheTTL > 5*time.Minute {
		t.Fatalf("cacheTTL = %v; long enough to freeze the number it is meant to refresh", cacheTTL)
	}

	calls := countingUsage(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, now)
	fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, now.Add(6*time.Minute))
	if *calls != 2 {
		t.Errorf("sent %d requests, want a six-minute-old entry refetched", *calls)
	}

	// The other direction is just as load-bearing: a TTL that expired instantly
	// would send a request per run and trip the rate limit it exists to dodge.
	fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, now.Add(6*time.Minute+time.Second))
	if *calls != 2 {
		t.Errorf("sent %d requests, want the fresh entry reused", *calls)
	}
}

// A stored entry that belongs to this sign-in but no longer parses must fall
// through to a request rather than be reported as the official window. The
// "payload is junk" case in the table above could not reach this: its fixture
// carried no account, so it was rejected one check earlier and tested the
// fingerprint a second time instead.
func TestCacheRefetchesWhenItsOwnPayloadStopsParsing(t *testing.T) {
	calls := countingUsage(t)
	creds := credentials{accessToken: "sk-tok"}

	if _, reason := fetchUsage(creds, window.Session, testNow); reason != "" {
		t.Fatalf("warm-up: %s", reason)
	}

	// Right account, right freshness — only the body is unusable.
	data, err := os.ReadFile(cachePath())
	if err != nil {
		t.Fatal(err)
	}
	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	entry.Body = json.RawMessage(`{"limits":[]}`)
	rewritten, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath(), rewritten, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, reason := fetchUsage(creds, window.Session, testNow); reason != "" {
		t.Fatalf("an unusable cached payload failed the run: %s", reason)
	}
	if *calls != 2 {
		t.Errorf("sent %d requests, want the unusable entry refetched", *calls)
	}
}

// Two runs racing — a shell prompt hook, a watch loop — must leave one whole
// entry, never half of two, and no temporary files behind.
func TestCacheSurvivesConcurrentWriters(t *testing.T) {
	countingUsage(t)
	creds := credentials{accessToken: "sk-tok"}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			writeCache(creds, []byte(`{"limits":[{"group":"session","percent":`+
				strconv.Itoa(i)+`,"resets_at":"2026-08-14T19:50:00Z"}]}`), testNow)
		}(i)
		wg.Add(1)
		go func() { defer wg.Done(); readCache(creds, testNow) }()
	}
	wg.Wait()

	body, ok := readCache(creds, testNow)
	if !ok {
		t.Fatal("no entry survived the race")
	}
	if _, reason := parseUsage(body, window.Session, testNow); reason != "" {
		t.Errorf("concurrent writers left a torn entry: %s", reason)
	}

	entries, err := os.ReadDir(filepath.Dir(cachePath()))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("left %d files behind, want only usage.json", len(entries))
	}
}

// A cache is an optimisation. Every way it can be broken must fall back to the
// request rather than fail the run.
func TestCacheDegradesToARequest(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"corrupt", "not json"},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := countingUsage(t)
			now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

			if err := os.MkdirAll(filepath.Dir(cachePath()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cachePath(), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}

			if _, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, now); reason != "" {
				t.Fatalf("a broken cache failed the run: %s", reason)
			}
			if *calls != 1 {
				t.Errorf("sent %d requests, want the broken cache bypassed", *calls)
			}
		})
	}
}

// Caching a failure would repeat it for the whole TTL, turning one bad
// response into minutes of them.
func TestCacheStoresOnlyUsablePayloads(t *testing.T) {
	calls := 0
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"limits":[]}`))
	})
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, now)
	fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, now)

	if calls != 2 {
		t.Errorf("sent %d requests, want an unusable payload not to be cached", calls)
	}
}

// The cache lands in a predictable path on disk. Anything that reads it must
// find quota percentages and nothing else.
func TestCacheNeverHoldsTheToken(t *testing.T) {
	countingUsage(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	fetchUsage(credentials{accessToken: "sk-secret"}, window.Session, now)

	data, err := os.ReadFile(cachePath())
	if err != nil {
		t.Fatalf("nothing was cached: %v", err)
	}
	if strings.Contains(string(data), "sk-secret") {
		t.Errorf("the cache holds the token:\n%s", data)
	}

	info, err := os.Stat(cachePath())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache mode = %v, want 0600", perm)
	}

	dir, err := os.Stat(filepath.Dir(cachePath()))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Errorf("cache directory mode = %v, want 0700", perm)
	}
}

// A read-only cache directory is someone else's problem, not a reason for the
// report to stop working.
func TestCacheSurvivesAnUnwritableDirectory(t *testing.T) {
	calls := countingUsage(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	if err := os.WriteFile(filepath.Dir(cachePath()), []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, reason := fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, now); reason != "" {
		t.Errorf("an unwritable cache failed the run: %s", reason)
	}
	if *calls != 1 {
		t.Errorf("sent %d requests, want the lookup to have happened anyway", *calls)
	}
}

// The credentials are checked before the cache is read, so a signed-out run
// reports that rather than serving a still-fresh answer. That is deliberate:
// the cached payload describes whichever account was signed in when it was
// written, and reporting another account's quota would be a wrong number that
// looks entirely right. A guess is honest; the wrong account's figure is not.
//
// Nothing else pins the order, and swapping it for one fewer file read would
// look like a harmless optimisation.
func TestCacheDoesNotOutliveTheSignIn(t *testing.T) {
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"sk-tok"}}`)
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(liveLimits))
	})

	if _, reason := Lookup(window.Session); reason != "" {
		t.Fatalf("warm-up lookup: %s", reason)
	}

	// The cache is fresh; the sign-in is gone.
	writeCredentials(t, "")

	got, reason := Lookup(window.Session)
	if reason == "" {
		t.Errorf("a cached figure was reported without a sign-in: %+v", got)
	}
	if got.PercentUsed != 0 {
		t.Errorf("PercentUsed = %v, want nothing carried over from the cache", got.PercentUsed)
	}
}

// A cached payload belongs to whichever account fetched it. Signing in as
// somebody else and reading their predecessor's figure would be a wrong number
// wearing the "official" label — the exact failure the whole lookup exists to
// avoid, and the one thing the sign-in check above does not catch: it proves a
// token is present, not that it is the same token.
func TestCacheDoesNotCrossAccounts(t *testing.T) {
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"token-of-account-A"}}`)

	var sentFor []string
	serveUsage(t, func(w http.ResponseWriter, r *http.Request) {
		sentFor = append(sentFor, r.Header.Get("authorization"))
		w.Write([]byte(`{"limits":[{"group":"session","percent":11,"resets_at":"2026-08-14T19:50:00Z"}]}`))
	})

	if _, reason := Lookup(window.Session); reason != "" {
		t.Fatalf("account A: %s", reason)
	}

	// Same machine, same cache, still inside the TTL — a different account.
	writeCredentials(t, `{"claudeAiOauth":{"accessToken":"token-of-account-B"}}`)
	if _, reason := Lookup(window.Session); reason != "" {
		t.Fatalf("account B: %s", reason)
	}

	if len(sentFor) != 2 {
		t.Fatalf("sent %d requests %v, want one per account", len(sentFor), sentFor)
	}
	if sentFor[1] != "Bearer token-of-account-B" {
		t.Errorf("the second lookup went out as %q, want account B's own token", sentFor[1])
	}
}

// A clock that jumps backwards dates the entry into the future, and a future
// entry is not fresh — it is unreadable. Left alone it would be served until
// the clock caught up.
func TestCacheRejectsAnEntryFromTheFuture(t *testing.T) {
	calls := countingUsage(t)
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)

	fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, now)
	fetchUsage(credentials{accessToken: "sk-tok"}, window.Session, now.Add(-time.Hour))

	if *calls != 2 {
		t.Errorf("sent %d requests, want an entry dated ahead of now refetched", *calls)
	}
}
