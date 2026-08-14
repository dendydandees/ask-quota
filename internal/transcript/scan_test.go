package transcript

import (
	"testing"
	"time"
)

const root = "../../testdata/projects"

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad test timestamp %q: %v", s, err)
	}
	return ts
}

func find(t *testing.T, sessions []Session, wantCWD string) Session {
	t.Helper()
	for _, s := range sessions {
		if s.CWD == wantCWD {
			return s
		}
	}
	t.Fatalf("no session with cwd %q in %d sessions", wantCWD, len(sessions))
	return Session{}
}

// Every assistant message is written ~3x per transcript. Summing without
// deduplicating by message id inflates every number in the tool by ~3x.
func TestScanDeduplicatesRepeatedMessages(t *testing.T) {
	since := mustParse(t, "2026-08-14T04:00:00Z")

	sessions, err := Scan(root, since)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := find(t, sessions, "/home/u/proj-a")
	if len(got.Messages) != 2 {
		t.Fatalf("got %d messages, want 2 (msg_dup counted once, msg_second once)", len(got.Messages))
	}

	var out int64
	for _, m := range got.Messages {
		out += m.Usage.Output
	}
	if want := int64(150); out != want {
		t.Errorf("output tokens = %d, want %d (300 would mean the 3x dedupe is broken)", out, want)
	}
}

// Transcripts are append-only logs; the final line may be mid-write.
func TestScanSkipsTruncatedLine(t *testing.T) {
	since := mustParse(t, "2026-08-14T04:00:00Z")

	sessions, err := Scan(root, since)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := find(t, sessions, "/home/u/proj-a")
	for _, m := range got.Messages {
		if m.ID == "msg_trunc" {
			t.Fatal("truncated final line was parsed; it must be skipped")
		}
	}
}

// A session may span the window boundary: only the in-window part counts.
func TestScanFiltersByMessageTimestamp(t *testing.T) {
	since := mustParse(t, "2026-08-14T04:00:00Z")

	sessions, err := Scan(root, since)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := find(t, sessions, "/home/u/proj-b")
	if len(got.Messages) != 1 || got.Messages[0].ID != "msg_new" {
		t.Fatalf("got %+v, want only msg_new; msg_old predates the window", got.Messages)
	}
}

// The label is what the user sees, so it must be the first real prompt, not a
// hook, a slash command, or a tool result.
func TestScanLabelsSessionWithFirstRealPrompt(t *testing.T) {
	since := mustParse(t, "2026-08-14T04:00:00Z")

	sessions, err := Scan(root, since)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := find(t, sessions, "/home/u/proj-a")
	if want := "fix the booking validation"; got.Label != want {
		t.Errorf("Label = %q, want %q", got.Label, want)
	}
}

func TestPromptTextRejectsMachinery(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"plain prompt", `"fix the booking validation"`, "fix the booking validation"},
		{"slash command", `"<command-name>/clear</command-name>"`, ""},
		{"task notification", `"<task-notification><task-id>x</task-id>"`, ""},
		{"system reminder", `"<system-reminder>note</system-reminder>"`, ""},
		{"interruption", `"[Request interrupted by user for tool use]"`, ""},
		{"resume notice", `"This session is being continued from a previous"`, ""},
		{"caveat", `"Caveat: The messages below were generated"`, ""},
		{"tool result block", `[{"type":"tool_result","content":"ok"}]`, ""},
		{"text block", `[{"type":"text","text":"add a filter"}]`, "add a filter"},
		{"empty", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := promptText([]byte(tc.content)); got != tc.want {
				t.Errorf("promptText(%s) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestScanIgnoresSessionsWithNoUsageInWindow(t *testing.T) {
	since := mustParse(t, "2026-08-14T07:00:00Z")

	sessions, err := Scan(root, since)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("got %d sessions, want 0 after the last message", len(sessions))
	}
}

func TestScanMissingRootIsNotAnError(t *testing.T) {
	sessions, err := Scan("../../testdata/does-not-exist", time.Time{})
	if err != nil {
		t.Fatalf("missing root should be empty, not an error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("got %d sessions, want 0", len(sessions))
	}
}
