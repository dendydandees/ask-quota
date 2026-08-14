# SPEC: drop the `quota-axi` dependency

Status: **proposed, not implemented.** Supersedes nothing; extends `docs/spec.md`,
which remains the product spec. This document covers one change: replacing the
external quota source with an in-process reader.

---

## 0. One correction before you approve

The stated goal is *"data tanpa tebakan"* — no guessing. This change does **not**
by itself remove guessing, and the spec would be dishonest if it claimed so.

`inferred` appears when there is no official window boundary. Removing the
shell-out does not create a boundary; it changes *where the boundary is fetched
from*. The `inferred` path must survive, because these still produce no
boundary:

- the OAuth token in `~/.claude/.credentials.json` has expired
- the machine is offline
- the endpoint answers `401` / `403` / `429`

What the change actually buys is narrower and still worth it:

| | Today | After |
|---|---|---|
| Guessing when everything works | no | no |
| Guessing because a separate tool was never installed | **yes** | no |
| Node.js on the machine | required for official data | not required |
| Who owns the upstream contract | `quota-axi` maintainer | you |

So: the win is that **no one can accidentally run in guess-mode by forgetting an
install step.** The guess-mode itself stays as the honest fallback. If you want
`inferred` to be impossible rather than rare, that is a different decision —
see §6, "Never".

---

## 1. Objective

Make `ask-quota` report official quota windows using only its own binary, with
no external command on `PATH` and no Node.js runtime.

**Users:** developers on Claude Code subscriptions who want to know which of
their sessions burned the quota. Single-user, local, Linux.

**Done when:** `quota-axi` can be uninstalled and every window label, boundary,
and `QUOTA` column is byte-identical to what it produced.

### Acceptance criteria

| # | Criterion | Verified by |
|---|---|---|
| A1 | With a valid token, `5h` and `week` report `official` with the same reset instant and percentage `quota-axi` reports | `TestLookupParsesLiveShape` against a recorded payload |
| A2 | `quota-axi` absent from `PATH` changes nothing about the output | `TestLookupIgnoresPath` |
| A3 | Expired token, offline, `401`, `403`, `429`, malformed JSON, and a 10 MB response body each degrade to the existing fallback and never return an error to the user | `TestLookupDegradesQuietly` table |
| A4 | The degrade is never silent: stderr names the reason | `TestSourceHintNamesTheReason` |
| A5 | The access token never reaches stdout, `--json`, stderr, or any host other than `api.anthropic.com` | `TestTokenNeverLeaves` + review |
| A6 | A run with no network completes in under the existing wall clock | timeout budget, §2 |

---

## 2. Design

### 2.1 The contract (verified 2026-08-14 against this account)

```
GET https://api.anthropic.com/api/oauth/usage
  authorization:  Bearer <accessToken>
  anthropic-beta: oauth-2025-04-20
  User-Agent:     claude-code/<version>
  accept:         application/json
```

Response, preferred shape — a `limits` array. Confirmed live: this account
returns three entries including a model-scoped one, so the array path is the
active one, not a legacy branch.

```jsonc
{"limits": [
  {"group": "session", "percent": 7,  "resets_at": "2026-08-14T19:50:00.233892+00:00"},
  {"group": "weekly",  "percent": 61, "resets_at": "2026-08-16T06:00:00.233910+00:00"},
  {"scope": {"model": {"id": "fable", "display_name": "Fable"}},
   "percent": 7, "resets_at": "..."}
]}
```

Fallback shape, when `limits` is absent — note the field is `utilization`, not
`percent`, and the timestamp key may be `reset_at`:

```jsonc
{"five_hour": {"utilization": 7, "resets_at": "..."},
 "seven_day": {"utilization": 61, "reset_at": "..."}}
```

Mapping to existing window kinds — unchanged from `internal/quota/quota.go`:

| Window | `limits` selector | Legacy key |
|---|---|---|
| `5h` | `group == "session"` | `five_hour` |
| `week` | `group == "weekly"` | `seven_day` |
| `30d` | — never requested | — |

Model-scoped entries (`scope.model`) are parsed and **discarded**. `ask-quota`
has no per-model window and inventing one is out of scope.

Timestamps carry six fractional digits; `time.Parse(time.RFC3339, …)` accepts
them. The current code already parses this exact string, so this is proven, not
assumed.

### 2.2 Credentials

Read `$CLAUDE_CONFIG_DIR/.credentials.json`, defaulting to `~/.claude`.

```jsonc
{"claudeAiOauth": {"accessToken": "...", "expiresAt": 1760000000000,
                   "subscriptionType": "max"}}
```

- Accept the token at `claudeAiOauth.accessToken`, or `accessToken` /
  `access_token` at the top level. All three are in the wild.
- `expiresAt` is **milliseconds**. If it is in the past, do not send the
  request — a request guaranteed to 401 is a wasted timeout.
- **No refresh.** `quota-axi` does not refresh either; it reports expiry and
  stops. Refreshing means minting tokens on the user's behalf, which is a much
  larger security surface than reading one. Expired token → `inferred`, with
  stderr saying to run `claude` once to re-authenticate.
- **macOS keychain: out of scope.** `README.md` already declares Linux the
  supported target. Adding a `security find-generic-password` shell-out to
  remove a shell-out would be self-defeating.

### 2.3 Where it goes

`internal/quota` keeps its exact public surface — `Official`, `Lookup(kind)
(Official, bool)`. Only the body changes. `cmd/ask-quota/main.go` is untouched
apart from the hint text, and `window.Resolve` does not change at all.

```
internal/quota/
  quota.go        Lookup: orchestration + the supported-window map (kept)
  credentials.go  NEW  read + validate the token, no network
  usage.go        NEW  the HTTP call and response parsing
  cache.go        NEW  the five-minute response cache (see 2.6)
  quota_test.go   rewritten around httptest
```

The `cappedBuffer` and the `exec` plumbing are **deleted**, not adapted:
`http.MaxBytesReader` and `http.Client.Timeout` replace them, both stdlib.

### 2.4 Timeouts

Existing budget is 8s, spent alongside the transcript scan so it costs no wall
clock. Keep 8s total via `http.Client{Timeout: 8 * time.Second}`. Dropping the
subprocess removes Node's ~200 ms startup, so this is strictly faster.

### 2.5 The new property nobody asked for

`ask-quota` currently makes **zero network calls**. After this change it makes
one, to `api.anthropic.com`, on every `5h` and `week` run. That is a genuine
change in the tool's character and `README.md`'s "no runtime, no dependencies"
line must stop implying otherwise.

Mitigation: honour `ASK_QUOTA_OFFLINE=1` to skip the call entirely and go
straight to `inferred`. One environment check, no flag, no config file.

---

### 2.6 The response cache

Approved during implementation, after the live parity check showed the endpoint
rate limits under a burst of runs — and a rate-limited run degrades to a guess,
which is the failure this whole change set out to remove. `quota-axi` had a
cache; inlining the lookup dropped it.

One GET answers every window, so the entry is keyed by nothing but the account:
a single payload serves `5h` and `week`. Five minutes is short against a
five-hour window and long enough to absorb a burst.

- Stored at `~/.cache/ask-quota/usage.json`, mode 0600, via `os.CreateTemp` plus
  `os.Rename` so two concurrent runs cannot leave a torn file.
- Carries a truncated SHA-256 of the access token. A different sign-in is a
  cache miss: serving the previous account's figure would be a wrong number
  wearing the `official` label.
- Only a payload that parsed is stored, so one bad response cannot repeat itself
  for the whole TTL.
- Every failure — missing, corrupt, stale, someone else's, dated ahead of now —
  is a plain miss that falls through to a request.
- Holds no credentials. Safe to delete at any time.

## 3. Commands

No new user-facing commands. Existing surface is frozen.

```sh
make build            # go build ./...
make test             # go test ./...
make fmt              # gofmt
make install          # binary + man page
go test ./internal/quota/ -run TestLookup -v
ASK_QUOTA_OFFLINE=1 ask-quota          # manual check of the fallback
```

---

## 4. Code style

Follow what is already in the repo — it is consistent and worth matching:

- Comments explain **why**, never what. The existing `WaitDelay` comment is the
  model: it names the failure it prevents.
- Failures in the optional path return `(zero, false)`, never an `error`. An
  absent quota source must never become the user's problem.
- Standard library only. `go.mod` has no requirements and must keep none.
- Exported identifiers carry doc comments; `go vet` and `gofmt -l` stay clean.

---

## 5. Testing strategy

`httptest.Server` for every network case — no live calls in CI, ever.

| Case | Expectation |
|---|---|
| `limits` array, both groups | correct reset + percent per window |
| legacy `five_hour` / `utilization` | same, via the fallback branch |
| both present | `limits` wins |
| `model:` entry only | `(zero, false)`, not a crash |
| `401` / `403` / `429` / `500` | `(zero, false)`, distinct stderr reasons |
| malformed JSON, empty body, HTML error page | `(zero, false)` |
| 10 MB body | bounded, no OOM, `(zero, false)` |
| server never responds | returns within the 8s budget |
| creds missing / unparseable / expired | no request is sent at all |
| a cached entry from another sign-in | ignored; a request is sent |
| a reset time already in the past | rejected rather than reported |
| `limits` present but missing the window | degrade; never fall back to the legacy keys |
| `ASK_QUOTA_OFFLINE=1` | no request is sent at all |
| token value | absent from every byte the tool writes |

Retain `capped_test.go`'s intent as the 10 MB case even though `cappedBuffer`
itself is deleted — the property it protected still matters.

**One end-to-end check stays real:** a `-tags=live` test comparing this
implementation's output against `quota-axi --json` on a developer machine. It
is the only thing that catches upstream changing the payload, and it must not
run in CI.

---

## 6. Boundaries

**Always**

- Degrade to `inferred` / `rolling` rather than fail. The report is the product;
  the official boundary is a garnish.
- Say on stderr *why* a run degraded. Silent degradation is the bug this whole
  thread started with.
- Treat the credential file as read-only. Never write it, never copy it, never
  include its contents in any output.
- Send the token to `api.anthropic.com` over TLS and nowhere else.

**Ask first**

- Adding any third-party module to `go.mod`.
- Surfacing model-scoped windows as a new user-facing feature.

**Never**

- Refresh, mint, or write OAuth tokens.
- Log, print, or serialise the access token, including in `--json` and errors.
- Fabricate an official boundary when none is available. If the choice is
  between "guess and label it `inferred`" and "invent a number that looks
  official", the guess wins every time. Removing the `inferred` label without
  removing the uncertainty would be the one change that makes this tool lie.
- Follow HTTP redirects to a non-`anthropic.com` host.

---

## 7. Out of scope

macOS keychain, token refresh, other providers (`codex`, `copilot`, `cursor`),
per-model windows, retries with backoff.

---

## 8. Migration

1. Implement behind the unchanged `quota.Lookup` signature.
2. Run both side by side on a real account; diff for one week of daily use.
3. Delete the `exec` path only after the diff is clean.
4. Update `README.md` (remove the `npm install -g quota-axi` section added
   today, state the network call), `man/ask-quota.1` `ENVIRONMENT`, and
   `CHANGELOG.md`.
5. `sourceHint` changes from "install `quota-axi`" to naming the actual reason:
   expired token, offline, rate-limited.
