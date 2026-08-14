package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dendy-fisiohome/ask-quota/internal/cli"
)

// writeSession adds one transcript under home for the given working directory.
func writeSession(t *testing.T, home, name, cwd, prompt string, output int64) {
	t.Helper()

	dir := filepath.Join(home, ".claude", "projects", strings.ReplaceAll(cwd, "/", "-"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	body := fmt.Sprintf(`{"type":"user","cwd":%q,"timestamp":%q,"message":{"content":%q}}`+"\n",
		cwd, now.Add(-time.Minute).Format(time.RFC3339Nano), prompt)
	body += fmt.Sprintf(`{"type":"assistant","cwd":%q,"timestamp":%q,"message":{"id":%q,"usage":{"output_tokens":%d}}}`+"\n",
		cwd, now.Format(time.RFC3339Nano), name, output)

	if err := os.WriteFile(filepath.Join(dir, name+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --group-by project must merge before --top truncates. Ranking first compares
// sessions rather than projects, so a project reports a partial total and one
// that only leads once its sessions are added up disappears entirely. Each
// stage is correct alone, so only an end-to-end test can see the ordering.
func TestRunGroupsByProjectBeforeRanking(t *testing.T) {
	home := t.TempDir()
	writeSession(t, home, "a1", "/home/u/alpha", "alpha part one", 30)
	writeSession(t, home, "a2", "/home/u/alpha", "alpha part two", 30)
	writeSession(t, home, "b1", "/home/u/beta", "beta all at once", 50)
	t.Setenv("HOME", home)
	t.Setenv("PATH", "")

	cfg, err := cli.Parse([]string{"--group-by", "project", "--top", "2"})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(cfg, &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d lines, want window + header + 2 project rows:\n%s", len(lines), out.String())
	}
	// alpha's two sessions total 60 against beta's 50, so alpha leads. Ranking
	// first would have compared 30 against 50 and put beta on top.
	if !strings.Contains(lines[2], "u/alpha") || !strings.Contains(lines[2], "2 sessions") {
		t.Errorf("top row = %q, want alpha merged from 2 sessions", lines[2])
	}
	if !strings.Contains(lines[3], "u/beta") {
		t.Errorf("second row = %q, want beta", lines[3])
	}
}
