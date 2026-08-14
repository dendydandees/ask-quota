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

	huge := strings.Repeat("x", 9*1024*1024)
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
