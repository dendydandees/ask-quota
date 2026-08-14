package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dendydandees/ask-quota/internal/cli"
	"github.com/dendydandees/ask-quota/internal/quota"
	"github.com/dendydandees/ask-quota/internal/window"
)

// fakeHome writes a transcript with the given prompt and recent activity, and
// points HOME at it.
//
// It also cuts every route to the real account. Overriding HOME is not enough:
// credentialsPath prefers $CLAUDE_CONFIG_DIR, so on a machine that sets it these
// tests would send the developer's live token to api.anthropic.com during an
// ordinary `go test`, and write the answer into their real cache.
func fakeHome(t *testing.T, prompt string, outputTokens int64) {
	t.Helper()

	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "-home-u-repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	var b strings.Builder
	fmt.Fprintf(&b, `{"type":"user","cwd":"/home/u/repo","timestamp":%q,"message":{"content":%q}}`+"\n",
		now.Add(-time.Minute).Format(time.RFC3339Nano), prompt)
	fmt.Fprintf(&b, `{"type":"assistant","cwd":"/home/u/repo","timestamp":%q,"message":{"id":"m1","usage":{"input_tokens":1,"output_tokens":%d,"cache_creation_input_tokens":2,"cache_read_input_tokens":3}}}`+"\n",
		now.Format(time.RFC3339Nano), outputTokens)

	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir()) // no token: these cover the degraded path
	t.Setenv("XDG_CACHE_HOME", t.TempDir())    // never touch the real cache
	t.Setenv("ASK_QUOTA_OFFLINE", "1")         // and never the network
}

func TestRunReportsASession(t *testing.T) {
	fakeHome(t, "fix the booking validation", 1234)

	var out bytes.Buffer
	cfg, err := cli.Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(cfg, &out, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := out.String()
	for _, want := range []string{"window 5h", "inferred", "SHARE", "fix the booking validation", "100.0%"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	// Without a quota source there is no honest number for this column.
	if strings.Contains(got, "QUOTA") {
		t.Errorf("QUOTA column present without a quota source:\n%s", got)
	}
}

func TestRunJSONIsPipeable(t *testing.T) {
	fakeHome(t, "add a filter", 4321)

	var out bytes.Buffer
	cfg, err := cli.Parse([]string{"--json"})
	if err != nil {
		t.Fatal(err)
	}
	if err := run(cfg, &out, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}

	var rows []struct {
		Share  float64 `json:"share"`
		Task   string  `json:"task"`
		Output int64   `json:"output_tokens"`
	}
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, out.String())
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Task != "add a filter" || rows[0].Output != 4321 {
		t.Errorf("got %+v, want the session verbatim", rows[0])
	}
}

func TestRunReportsAnEmptyWindowWithoutFailing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("ASK_QUOTA_OFFLINE", "1")

	var out bytes.Buffer
	cfg, err := cli.Parse([]string{"30d"})
	if err != nil {
		t.Fatal(err)
	}
	if err := run(cfg, &out, io.Discard); err != nil {
		t.Fatalf("an empty corpus must not be an error: %v", err)
	}
	if !strings.Contains(out.String(), "No Claude Code usage found") {
		t.Errorf("want guidance for an empty result, got:\n%s", out.String())
	}
}

func TestWindowLineDistinguishesOfficialFromInferred(t *testing.T) {
	start := time.Date(2026, 8, 14, 9, 20, 0, 0, time.UTC)

	inferred := windowLine(window.Session, start, quota.Official{}, false)
	if !strings.Contains(inferred, "inferred") || strings.Contains(inferred, "used") {
		t.Errorf("inferred line = %q, want it marked inferred with no percentage", inferred)
	}

	got := windowLine(window.Week, start, quota.Official{PercentUsed: 58}, true)
	if !strings.Contains(got, "official") || !strings.Contains(got, "58% used") {
		t.Errorf("official line = %q, want the source and percentage named", got)
	}

	// 30d has no upstream window and nothing to infer: it is a plain rolling
	// span, and calling it inferred claimed an uncertainty that is not there.
	month := windowLine(window.Month, start, quota.Official{}, false)
	if !strings.Contains(month, "rolling") || strings.Contains(month, "inferred") {
		t.Errorf("30d line = %q, want it marked rolling", month)
	}
}

// Without this the tool degrades silently: the QUOTA column simply is not
// there, and nothing on screen says why or whether it is fixable.
func TestSourceHintOnlyWhereTheRunWasDegraded(t *testing.T) {
	const reason = "Claude Code sign-in has expired; run claude once to renew it"

	if hint := sourceHint(window.Session, reason); !strings.Contains(hint, reason) {
		t.Errorf("hint = %q, want the reason carried through unchanged", hint)
	}
	if hint := sourceHint(window.Month, reason); hint != "" {
		t.Errorf("30d hint = %q, want none: it never asks for an official boundary", hint)
	}
	if hint := sourceHint(window.Session, ""); hint != "" {
		t.Errorf("hint = %q, want none when the boundary is already official", hint)
	}
}

// A `go install` build never runs the release ldflags, so the version must come
// from the module info the toolchain records rather than reporting "dev".
func TestVersionFallsBackToBuildInfo(t *testing.T) {
	original := version
	t.Cleanup(func() { version = original })

	version = "v1.2.3"
	if got := versionString(); got != "v1.2.3" {
		t.Errorf("versionString() = %q, want the injected version", got)
	}

	version = ""
	if got := versionString(); got == "" {
		t.Error("versionString() is empty without an injected version")
	}
}

// The reason a run degraded goes to stderr, and it does go somewhere.
//
// Both halves matter. Asserting only that stdout stays clean is satisfied
// perfectly by a run that says nothing at all — deleting the print entirely
// would pass — and that silent degrade is the whole thing the hint exists to
// prevent. Asserting only that stderr carries it would miss the layout damage:
// anything parsing stdout reads the window line, then the header, then rows,
// and a sentence slipped between them shifts all of it.
func TestRunReportsWhyItDegradedOnStderrOnly(t *testing.T) {
	fakeHome(t, "fix the booking validation", 1234)

	var out, errOut bytes.Buffer
	cfg, err := cli.Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(cfg, &out, &errOut); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Confirm the run really did degrade, or this test proves nothing.
	if !strings.Contains(out.String(), "inferred") {
		t.Fatalf("this run was not degraded, so the check is vacuous:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "ask-quota:") {
		t.Errorf("the run degraded silently; stderr held %q", errOut.String())
	}
	if strings.Contains(out.String(), "ask-quota:") {
		t.Errorf("the degrade reason reached stdout:\n%s", out.String())
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d lines, want window + header + 1 row:\n%s", len(lines), out.String())
	}
}

// An empty window and --json are degraded runs too, and both returned before
// the hint was ever printed. The empty-window line is the worse of the two: it
// quotes a start time that may itself be a guess.
func TestRunReportsWhyItDegradedOnEveryExitPath(t *testing.T) {
	for _, args := range [][]string{{"--json"}, {"30d"}, nil} {
		fakeHome(t, "fix the booking validation", 1234)

		cfg, err := cli.Parse(args)
		if err != nil {
			t.Fatal(err)
		}
		var out, errOut bytes.Buffer
		if err := run(cfg, &out, &errOut); err != nil {
			t.Fatalf("%v: %v", args, err)
		}

		// 30d is not degraded by lacking a boundary — it never asks for one —
		// so it is the one window that stays quiet.
		want := cfg.Window != window.Month
		if got := strings.Contains(errOut.String(), "ask-quota:"); got != want {
			t.Errorf("%v: stderr = %q, want a reason: %v", args, errOut.String(), want)
		}
	}
}
