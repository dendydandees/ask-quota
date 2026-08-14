package transcript

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeUsage lays down a transcript holding one assistant message.
func writeUsage(t *testing.T, dir, name string, at time.Time, id string, output int64) {
	t.Helper()

	body := fmt.Sprintf(`{"type":"user","cwd":"/home/u/p","timestamp":%q,"message":{"content":%q}}`+"\n",
		at.Format(time.RFC3339Nano), name)
	body += fmt.Sprintf(`{"type":"assistant","cwd":"/home/u/p","timestamp":%q,"message":{"id":%q,"usage":{"output_tokens":%d}}}`+"\n",
		at.Add(time.Second).Format(time.RFC3339Nano), id, output)
	if err := os.WriteFile(filepath.Join(dir, name+".jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The in-file rule — the most complete copy of a message wins — has to hold
// across files too. An interrupted transcript keeps a partial copy of its last
// turn; the resume replays that turn complete. Claiming for the earlier session
// without comparing the copies keeps the partial one and undercounts.
func TestScanKeepsTheCompleteCopyAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().UTC().Add(-time.Hour)

	writeUsage(t, dir, "interrupted", base, "m", 3)
	writeUsage(t, dir, "resumed", base.Add(time.Minute), "m", 378)

	sessions, err := Scan(dir, base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got, want := totalOutput(sessions), int64(378); got != want {
		t.Errorf("output tokens = %d, want %d (3 means the interrupted partial copy won)", got, want)
	}
	// Upgrading the numbers must not move the message to the later session.
	if len(sessions) != 1 || sessions[0].Label != "interrupted" {
		t.Errorf("got %d sessions labelled %q, want the earlier one to keep ownership",
			len(sessions), sessions[0].Label)
	}
}

// One transcript the process cannot open must cost that file only. Scan reads
// files concurrently into a shared slice, so an error path that returned early
// would drop every other session with it.
func TestScanSkipsAnUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a 0000 file")
	}
	dir := t.TempDir()
	base := time.Now().UTC().Add(-time.Hour)

	writeTranscript(t, dir, "readable", "the one that counts", base, "a")
	writeTranscript(t, dir, "locked", "unreadable", base.Add(time.Minute), "b")
	if err := os.Chmod(filepath.Join(dir, "locked.jsonl"), 0o000); err != nil {
		t.Fatal(err)
	}

	sessions, err := Scan(dir, base.Add(-time.Hour))
	if err != nil {
		t.Fatalf("an unreadable transcript must not fail the scan: %v", err)
	}
	if len(sessions) != 1 || sessions[0].Label != "the one that counts" {
		t.Fatalf("got %d sessions, want only the readable one", len(sessions))
	}
}

// Dropping an oversized line is only half the contract: it must also be
// consumed. Returning early leaves the reader mid-line, so its tail is read as
// if it were the next line — which usually parses as junk and is silently
// skipped, hiding the defect until a tail happens to look like a record.
//
// The overshoot is deliberately larger than the read buffer: a line that goes
// over inside its final chunk is drained by accident and pins nothing.
func TestReadCappedLineConsumesTheLineItDrops(t *testing.T) {
	restore := maxLine
	maxLine = 4 << 10
	t.Cleanup(func() { maxLine = restore })

	src := strings.Repeat("x", maxLine+64<<10) + "\nthe next line\n"
	br := bufio.NewReaderSize(strings.NewReader(src), 4<<10)

	line, err := readCappedLine(br)
	if line != nil || err != nil {
		t.Fatalf("oversized line = %d bytes, err %v; want it dropped cleanly", len(line), err)
	}

	line, _ = readCappedLine(br)
	if got, want := string(line), "the next line\n"; got != want {
		t.Errorf("next line = %q, want %q — the dropped line was not fully consumed", got, want)
	}
}

// The cap is a ceiling, not a limit one byte lower.
func TestReadCappedLineKeepsALineAtTheCap(t *testing.T) {
	restore := maxLine
	maxLine = 4 << 10
	t.Cleanup(func() { maxLine = restore })

	src := strings.Repeat("x", maxLine-1) + "\n"
	br := bufio.NewReaderSize(strings.NewReader(src), 1<<10)

	line, _ := readCappedLine(br)
	if len(line) != maxLine {
		t.Errorf("kept %d bytes, want the full %d-byte line", len(line), maxLine)
	}
}
