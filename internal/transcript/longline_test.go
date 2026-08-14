package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A single oversized line used to stop the scanner outright, silently dropping
// every message after it. Transcripts can carry pasted images, so the line
// below is larger than any fixed buffer this package once used.
func TestScanSurvivesAnOversizedLine(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	restore := maxLine
	maxLine = 4 << 10
	t.Cleanup(func() { maxLine = restore })

	huge := strings.Repeat("x", maxLine*2)
	var b strings.Builder
	fmt.Fprintf(&b, `{"type":"user","cwd":"/home/u/repo","timestamp":%q,"message":{"content":"real prompt"}}`+"\n",
		now.Add(-2*time.Minute).Format(time.RFC3339Nano))
	fmt.Fprintf(&b, `{"type":"user","cwd":"/home/u/repo","timestamp":%q,"message":{"content":%q}}`+"\n",
		now.Add(-time.Minute).Format(time.RFC3339Nano), huge)
	fmt.Fprintf(&b, `{"type":"assistant","cwd":"/home/u/repo","timestamp":%q,"message":{"id":"after","usage":{"output_tokens":42}}}`+"\n",
		now.Format(time.RFC3339Nano))

	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	sessions, err := Scan(dir, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 — the message after the huge line was dropped", len(sessions))
	}
	if got := sessions[0].Messages; len(got) != 1 || got[0].ID != "after" {
		t.Fatalf("got %+v, want the message that follows the oversized line", got)
	}
	if sessions[0].Label != "real prompt" {
		t.Errorf("Label = %q, want the prompt before the oversized line", sessions[0].Label)
	}
}

// Reading a line with no ceiling at all trades silent truncation for an OOM:
// one 600 MB line costs ~4x that in RSS, times NumCPU concurrent files. A line
// past the cap is skipped; the messages after it still count.
func TestScanSkipsALineBeyondTheCap(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()

	restore := maxLine
	maxLine = 4 << 10
	t.Cleanup(func() { maxLine = restore })

	var b strings.Builder
	fmt.Fprintf(&b, `{"type":"user","cwd":"/home/u/repo","timestamp":%q,"message":{"content":"real prompt"}}`+"\n",
		now.Add(-2*time.Minute).Format(time.RFC3339Nano))
	// A well-formed message that happens to exceed the cap: it must be dropped
	// rather than counted, and must not take the rest of the file with it.
	fmt.Fprintf(&b, `{"type":"assistant","cwd":"/home/u/repo","timestamp":%q,"message":{"id":"huge","usage":{"output_tokens":999},"pad":%q}}`+"\n",
		now.Add(-time.Minute).Format(time.RFC3339Nano), strings.Repeat("x", maxLine+1024))
	fmt.Fprintf(&b, `{"type":"assistant","cwd":"/home/u/repo","timestamp":%q,"message":{"id":"after","usage":{"output_tokens":42}}}`+"\n",
		now.Format(time.RFC3339Nano))

	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	sessions, err := Scan(dir, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	got := sessions[0].Messages
	if len(got) != 1 || got[0].ID != "after" {
		t.Fatalf("got %+v, want only the message after the oversized line", got)
	}
}
