package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTranscript lays down a transcript whose assistant messages carry the
// given ids, all stamped at base plus their index.
func writeTranscript(t *testing.T, dir, name, label string, base time.Time, ids ...string) {
	t.Helper()

	body := fmt.Sprintf(`{"type":"user","cwd":"/home/u/%s","timestamp":%q,"message":{"content":%q}}`+"\n",
		name, base.Format(time.RFC3339Nano), label)
	for i, id := range ids {
		body += fmt.Sprintf(`{"type":"assistant","cwd":"/home/u/%s","timestamp":%q,"message":{"id":%q,"usage":{"output_tokens":100}}}`+"\n",
			name, base.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano), id)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func totalOutput(sessions []Session) int64 {
	var n int64
	for _, s := range sessions {
		for _, m := range s.Messages {
			n += m.Usage.Output
		}
	}
	return n
}

// Resuming a conversation writes a fresh transcript that replays earlier turns
// with their original ids and timestamps. Counting them in both files inflates
// the window total — the same defect as the 3x per-file duplication, one scope
// wider.
func TestScanDeduplicatesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().UTC().Add(-time.Hour)

	writeTranscript(t, dir, "original", "start the feature", base, "a", "b")
	writeTranscript(t, dir, "resumed", "continue", base.Add(time.Minute), "a", "b", "c")

	sessions, err := Scan(dir, base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if got, want := totalOutput(sessions), int64(300); got != want {
		t.Errorf("output tokens = %d, want %d (500 means a and b were counted twice)", got, want)
	}
}

// A message belongs to the session that saw it first, so the usage lands on the
// conversation that originally incurred it.
func TestScanAttributesADuplicateToTheEarlierSession(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().UTC().Add(-time.Hour)

	writeTranscript(t, dir, "original", "start the feature", base, "a", "b")
	writeTranscript(t, dir, "resumed", "continue", base.Add(time.Minute), "a", "b", "c")

	sessions, err := Scan(dir, base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	byLabel := map[string]int{}
	for _, s := range sessions {
		byLabel[s.Label] = len(s.Messages)
	}
	if byLabel["start the feature"] != 2 {
		t.Errorf("earlier session kept %d messages, want 2", byLabel["start the feature"])
	}
	if byLabel["continue"] != 1 {
		t.Errorf("later session kept %d messages, want 1 (only its own)", byLabel["continue"])
	}
}

// A transcript that only replays another's messages contributes nothing, so it
// must vanish rather than linger as a zero row.
func TestScanDropsASessionThatIsEntirelyAReplay(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().UTC().Add(-time.Hour)

	writeTranscript(t, dir, "original", "the real one", base, "a", "b", "c")
	writeTranscript(t, dir, "subset", "a fork of it", base.Add(time.Minute), "a", "b")

	sessions, err := Scan(dir, base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 — the pure replay must disappear", len(sessions))
	}
	if sessions[0].Label != "the real one" {
		t.Errorf("kept %q, want the original", sessions[0].Label)
	}
}

// Attribution must not depend on the order the filesystem hands files back.
func TestScanAttributionIsDeterministic(t *testing.T) {
	base := time.Now().UTC().Add(-time.Hour)

	for i := range 5 {
		dir := t.TempDir()
		// Names chosen so lexical order and chronological order disagree.
		writeTranscript(t, dir, "zzz-first", "earliest", base, "shared")
		writeTranscript(t, dir, "aaa-second", "later", base.Add(time.Minute), "shared")

		sessions, err := Scan(dir, base.Add(-time.Hour))
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		if len(sessions) != 1 || sessions[0].Label != "earliest" {
			t.Fatalf("run %d: got %d sessions labelled %q, want 1 labelled \"earliest\"",
				i, len(sessions), sessions[0].Label)
		}
	}
}

// The three copies of an assistant message are streaming snapshots, not
// identical rows: the first is written before the turn finishes. Keeping the
// first copy undercounts output by ~7.5% on a real corpus.
func TestScanKeepsTheCompleteCopyOfARepeatedMessage(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().UTC().Add(-time.Hour)

	body := fmt.Sprintf(`{"type":"user","cwd":"/home/u/p","timestamp":%q,"message":{"content":"a prompt"}}`+"\n",
		base.Format(time.RFC3339Nano))
	for i, out := range []int{3, 3, 378} {
		body += fmt.Sprintf(`{"type":"assistant","cwd":"/home/u/p","timestamp":%q,"message":{"id":"m","usage":{"output_tokens":%d,"cache_read_input_tokens":%d}}}`+"\n",
			base.Add(time.Duration(i)*time.Second).Format(time.RFC3339Nano), out, out*10)
	}
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	sessions, err := Scan(dir, base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sessions) != 1 || len(sessions[0].Messages) != 1 {
		t.Fatalf("got %d sessions, want 1 with 1 message", len(sessions))
	}

	got := sessions[0].Messages[0].Usage
	if got.Output != 378 {
		t.Errorf("Output = %d, want 378 (3 means the partial first copy won)", got.Output)
	}
	if got.CacheRead != 3780 {
		t.Errorf("CacheRead = %d, want 3780 — the whole record must come from the same copy", got.CacheRead)
	}
}
