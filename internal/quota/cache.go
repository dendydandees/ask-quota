package quota

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// cacheTTL is short on purpose. The cache exists to absorb a burst of runs —
// the endpoint rate limits quickly, and being limited means falling back to a
// guess — not to freeze the number. Against a 5-hour window, five minutes of
// staleness is invisible; against the rate limit it is the whole point.
const cacheTTL = 5 * time.Minute

// cacheEntry is one stored response. The whole payload is kept rather than the
// parsed window: a single GET answers every window, so one entry serves 5h and
// week alike.
type cacheEntry struct {
	FetchedAt time.Time       `json:"fetched_at"`
	Account   string          `json:"account"`
	Body      json.RawMessage `json:"body"`
}

// fingerprint identifies which sign-in a cached payload belongs to without
// storing the token that fetched it. Signing in as somebody else must not serve
// their predecessor's figure: it would be a wrong number wearing the
// "official" label, which is worse than the guess it replaced.
//
// Truncated because it only has to tell one account from another. It is not a
// credential and cannot be turned back into one.
func fingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

// cachePath is under the user's cache directory, which os.UserCacheDir already
// resolves from XDG_CACHE_HOME. Empty when there is nowhere to put it, which
// simply means no caching.
func cachePath() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "ask-quota", "usage.json")
}

// readCache returns a stored payload that is still fresh and belongs to this
// sign-in. Every failure — missing, corrupt, stale, someone else's — is a plain
// miss: a cache that cannot be trusted is exactly as useful as one that is
// empty.
func readCache(creds credentials, now time.Time) ([]byte, bool) {
	path := cachePath()
	if path == "" {
		return nil, false
	}

	// Bounded like the network body, and for the same reason: the cache
	// directory comes from an environment variable, so its contents are not
	// ours to assume are small.
	data, err := readAtMost(path, maxBody)
	if err != nil {
		return nil, false
	}

	var entry cacheEntry
	if json.Unmarshal(data, &entry) != nil || len(entry.Body) == 0 {
		return nil, false
	}
	if entry.Account != fingerprint(creds.accessToken) {
		return nil, false
	}
	// After(now) as well as the TTL: a clock that jumps backwards dates the
	// entry into the future, where the age is negative and every check for
	// staleness passes until the clock catches up.
	if entry.FetchedAt.After(now) || now.Sub(entry.FetchedAt) >= cacheTTL {
		return nil, false
	}
	return entry.Body, true
}

// writeCache stores a payload, best effort. A read-only or missing cache
// directory costs one request per run and nothing else, so none of it is worth
// reporting — and it must never be worth failing a run for.
//
// The payload holds quota percentages and reset times. The token that fetched
// it is not part of the response and never reaches this file — only its
// fingerprint does — but the file is written 0600 regardless: what upstream
// puts in a payload is not ours to promise.
func writeCache(creds credentials, body []byte, now time.Time) {
	path := cachePath()
	if path == "" {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return
	}

	data, err := json.Marshal(cacheEntry{
		FetchedAt: now,
		Account:   fingerprint(creds.accessToken),
		Body:      body,
	})
	if err != nil {
		return
	}

	// Written beside the target and renamed over it: two ask-quota runs racing
	// would otherwise interleave into one torn file. Rename is atomic within a
	// directory, so a reader sees the old entry or the new one, never half of
	// either. os.CreateTemp already opens at 0600.
	tmp, err := os.CreateTemp(filepath.Dir(path), "usage-*.json")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeds

	_, writeErr := tmp.Write(data)
	if closeErr := tmp.Close(); writeErr != nil || closeErr != nil {
		return
	}
	_ = os.Rename(tmp.Name(), path)
}
