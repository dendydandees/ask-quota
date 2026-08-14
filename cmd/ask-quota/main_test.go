package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dendy-fisiohome/ask-quota/internal/cli"
	"github.com/dendy-fisiohome/ask-quota/internal/quota"
	"github.com/dendy-fisiohome/ask-quota/internal/window"
)

// fakeHome writes a transcript with the given prompt and recent activity, and
// points HOME at it. PATH is emptied so the optional quota source cannot be
// found: these tests cover the standalone path.
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
	t.Setenv("PATH", "")
}

func TestRunReportsASession(t *testing.T) {
	fakeHome(t, "fix the booking validation", 1234)

	var out bytes.Buffer
	cfg, err := cli.Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := run(cfg, &out); err != nil {
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
	if err := run(cfg, &out); err != nil {
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
	t.Setenv("PATH", "")

	var out bytes.Buffer
	cfg, err := cli.Parse([]string{"30d"})
	if err != nil {
		t.Fatal(err)
	}
	if err := run(cfg, &out); err != nil {
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
}
