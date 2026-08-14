package quota

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"
)

// maxCredentials bounds the credentials read. The real file is a few hundred
// bytes; the path comes from an environment variable, so it is not ours to
// assume is small.
const maxCredentials = 64 << 10

// readAtMost reads a file up to a ceiling. Both files ask-quota reads sit at
// paths an environment variable can move, so neither is read unbounded.
func readAtMost(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}

// credentials is the OAuth token Claude Code already stores on this machine.
// ask-quota only ever reads it: it never refreshes, writes or forwards it
// anywhere but the usage endpoint.
type credentials struct {
	accessToken string
}

// oauthFields is the subset of the credentials file ask-quota needs. The token
// appears under three different keys depending on how the account was set up,
// so all three are read rather than assuming the current one.
type oauthFields struct {
	AccessToken  string `json:"accessToken"`
	AccessToken2 string `json:"access_token"`
	// ExpiresAt is milliseconds since the epoch. Reading it as seconds would
	// date every live token to 1970 and degrade every run.
	ExpiresAt int64 `json:"expiresAt"`
}

func (f oauthFields) token() string {
	if f.AccessToken != "" {
		return f.AccessToken
	}
	return f.AccessToken2
}

// credentialsPath is where Claude Code keeps the token, honouring the same
// override the CLI itself uses.
func credentialsPath() string {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".claude")
	}
	return filepath.Join(dir, ".credentials.json")
}

// readCredentials loads the stored token. The returned string is a reason to
// show the user, empty exactly when the token is usable — a reason rather than
// an error because none of these are failures the run should stop for, and
// because a plain sentence is what ends up on stderr.
//
// It never includes the token: the reason is printed, and printed text reaches
// scrollback, screenshots and pasted bug reports.
func readCredentials(now time.Time) (credentials, string) {
	path := credentialsPath()
	if path == "" {
		return credentials{}, "no home directory to read Claude Code credentials from"
	}

	data, err := readAtMost(path, maxCredentials)
	if err != nil {
		return credentials{}, "not signed in to Claude Code"
	}

	// Two passes over a small file, rather than an embedded struct: the nested
	// and flat layouts are both real, and one unmarshal cannot express "prefer
	// nested, fall back to top level".
	//
	// Nested wins outright when it yields a token. Rejecting on either pass
	// failing would let a stray top-level key of the wrong type veto a nested
	// token that parsed perfectly well — the opposite of what the two passes
	// are for.
	var fields oauthFields
	var nested struct {
		OAuth *oauthFields `json:"claudeAiOauth"`
	}
	if json.Unmarshal(data, &nested) == nil && nested.OAuth != nil {
		fields = *nested.OAuth
	}
	if fields.token() == "" {
		var flat oauthFields
		if json.Unmarshal(data, &flat) != nil {
			return credentials{}, "Claude Code credentials are unreadable"
		}
		fields = flat
	}

	if fields.token() == "" {
		return credentials{}, "Claude Code credentials hold no access token"
	}

	// An absent expiry is not an expiry at the epoch: the field is optional,
	// and treating zero as "long past" would reject every file without it.
	if fields.ExpiresAt != 0 && !time.UnixMilli(fields.ExpiresAt).After(now) {
		return credentials{}, "Claude Code sign-in has expired; run claude once to renew it"
	}

	return credentials{accessToken: fields.token()}, ""
}
