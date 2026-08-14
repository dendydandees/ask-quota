package quota

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/dendydandees/ask-quota/internal/window"
)

const (
	defaultUsageURL = "https://api.anthropic.com/api/oauth/usage"
	oauthBeta       = "oauth-2025-04-20"
	userAgent       = "ask-quota"

	// maxBody bounds the response, ~1000x the real payload. The endpoint is
	// trusted, but a captive portal or proxy answering in its place is not.
	maxBody = 1 << 20

	// usageTimeout is the whole call. It is generous because the lookup runs
	// alongside the transcript scan and so costs no wall clock.
	usageTimeout = 8 * time.Second
)

// usageURL is a variable so tests can point it at an httptest server. A live
// call in the suite would need a real token and fail on any machine lacking
// one.
var usageURL = defaultUsageURL

// errRedirect rejects any redirect. The token authenticates every request, so
// following one blindly hands it to whoever controls the target, and the
// endpoint has no reason to redirect in the first place.
var errRedirect = errors.New("refusing to follow a redirect while carrying credentials")

// usageClient stops at the first redirect rather than following it.
var usageClient = &http.Client{
	Timeout: usageTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return errRedirect
	},
}

// fetchUsage asks the API about one window. Like everything on this path a
// failure is simply absence — the report still ranks sessions, it just cannot
// scale them to the real window. The returned string says why, empty exactly
// when the window was found, and is written to be shown to the user.
func fetchUsage(creds credentials, kind string, now time.Time) (Official, string) {
	if _, ok := limitGroup[kind]; !ok {
		return Official{}, noUpstreamWindow(kind)
	}

	// One GET answers every window, so a cached payload serves whichever one
	// is asked for next. Without this the endpoint rate limits a burst of runs
	// and each limited run falls back to a guess.
	if body, ok := readCache(creds, now); ok {
		if got, reason := parseUsage(body, kind, now); reason == "" {
			return got, ""
		}
	}

	req, err := http.NewRequest(http.MethodGet, usageURL, nil)
	if err != nil {
		return Official{}, "the quota endpoint is unusable"
	}
	req.Header.Set("authorization", "Bearer "+creds.accessToken)
	req.Header.Set("anthropic-beta", oauthBeta)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("accept", "application/json")

	resp, err := usageClient.Do(req)
	if err != nil {
		if errors.Is(err, errRedirect) {
			return Official{}, "the quota endpoint redirected elsewhere"
		}
		return Official{}, "could not reach the quota API"
	}
	defer resp.Body.Close()
	if reason := statusReason(resp.StatusCode); reason != "" {
		return Official{}, reason
	}

	// MaxBytesReader truncates rather than allocating the whole body, so an
	// endless response cannot exhaust memory before the timeout fires. A
	// truncated body fails to parse, which is the outcome we want anyway.
	data, err := io.ReadAll(http.MaxBytesReader(nil, resp.Body, maxBody))
	if err != nil {
		return Official{}, "the quota API answered with more than we will read"
	}

	// Cached only once it has proved usable: storing a response that does not
	// parse would repeat that failure for the whole TTL.
	got, reason := parseUsage(data, kind, now)
	if reason == "" {
		writeCache(creds, data, now)
	}
	return got, reason
}

// statusReason turns a response code into words, empty when the response is
// usable. The codes are separated because they have different fixes: signing in
// again, waiting, or nothing the user can do.
func statusReason(code int) string {
	switch code {
	case http.StatusOK:
		return ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return "the quota API rejected the sign-in; run claude once to renew it"
	case http.StatusTooManyRequests:
		return "the quota API is rate limiting this account"
	default:
		return "the quota API is unavailable"
	}
}

// usagePayload covers both shapes the endpoint returns. limits is the vendor's
// own list of every window the account has and is preferred: a window added
// upstream shows up there without a code change.
type usagePayload struct {
	Limits   []limitEntry  `json:"limits"`
	FiveHour *legacyWindow `json:"five_hour"`
	SevenDay *legacyWindow `json:"seven_day"`
}

// limitEntry is one window from the limits array.
type limitEntry struct {
	Group    string  `json:"group"`
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resets_at"`

	// Scope is null for the account's own windows and an object for one scoped
	// to something narrower. Its presence alone is the signal, so there is
	// nothing to look inside for: a live payload carries both a weekly_all
	// entry and a weekly_scoped one under the same group, and the scoped one
	// names its model under a null id.
	Scope *struct{} `json:"scope"`
}

// legacyWindow is the older per-window object: the percentage is named
// utilization, and the reset key appears both ways.
type legacyWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
	ResetAt     string  `json:"reset_at"`
}

func (w legacyWindow) resets() string {
	if w.ResetsAt != "" {
		return w.ResetsAt
	}
	return w.ResetAt
}

// limitGroup maps a report window to the group name used inside limits. Its
// keys are also the set of windows that exist upstream at all, which is what
// every caller checks before going any further.
var limitGroup = map[string]string{
	window.Session: "session",
	window.Week:    "weekly",
}

// noUpstreamWindow is the answer for 30d, which mirrors nothing the service
// enforces. Written once because three layers reject it — Lookup before
// anything, fetchUsage before the network, parseUsage before the empty group
// could match an entry carrying none.
func noUpstreamWindow(kind string) string {
	return "no upstream window exists for " + kind
}

func parseUsage(data []byte, kind string, now time.Time) (Official, string) {
	// Looked up rather than indexed: a miss yields the empty group, which
	// would then match any entry that happens to carry no group of its own.
	group, ok := limitGroup[kind]
	if !ok {
		return Official{}, noUpstreamWindow(kind)
	}

	var payload usagePayload
	if json.Unmarshal(data, &payload) != nil {
		return Official{}, "the quota API answered with something unreadable"
	}

	for _, l := range payload.Limits {
		// A scoped entry is some narrower window — one model, one surface —
		// not the account's. Reading it as the account window would report a
		// plausible-looking but wrong reset and percentage.
		if l.Scope != nil {
			continue
		}
		if l.Group == group {
			return official(l.Percent, l.ResetsAt, now)
		}
	}

	// The older keys answer only for a payload that has no limits array at all.
	// Falling back per window instead would let one renamed group — or a group
	// whose only entry is scoped — quietly return the legacy figure, which
	// comes back with no reason and so gets printed as "official". A payload
	// that lists its windows and does not list this one has told us something,
	// and the honest reading is that we no longer know the boundary.
	if len(payload.Limits) > 0 {
		return Official{}, "the quota API listed no " + kind + " window"
	}

	legacy := payload.FiveHour
	if kind == window.Week {
		legacy = payload.SevenDay
	}
	if legacy == nil {
		return Official{}, "the quota API reported no " + kind + " window"
	}
	return official(legacy.Utilization, legacy.resets(), now)
}

// official builds the result, rejecting a timestamp it cannot read rather than
// reporting a zero reset that the resolver would treat as "no boundary" while
// the caller believed it had one.
//
// The percentage is clamped rather than rejected. It is multiplied into every
// QUOTA cell, so an out-of-range figure would print a confidently wrong column;
// clamping keeps the report usable, and extra usage can legitimately push past
// the limit without the bar meaning anything beyond "full".
func official(percent float64, resets string, now time.Time) (Official, string) {
	at, err := time.Parse(time.RFC3339, resets)
	if err != nil {
		return Official{}, "the quota API sent a reset time we cannot read"
	}
	// A boundary that has already passed is not a boundary. Accepting one would
	// hand window.Resolve a start time in the past and report the wrong window
	// with full confidence.
	if !at.After(now) {
		return Official{}, "the quota API sent a reset time that has already passed"
	}
	return Official{ResetsAt: at, PercentUsed: min(max(percent, 0), 100)}, ""
}
