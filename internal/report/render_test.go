package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dendy-fisiohome/ask-quota/internal/transcript"
)

func TestRankSortsByCostDescending(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session("small", "/p/a", transcript.Usage{Output: 1}),
		session("big", "/p/b", transcript.Usage{Output: 100}),
		session("mid", "/p/c", transcript.Usage{Output: 10}),
	})

	got := Rank(rows, 0)
	want := []string{"big", "mid", "small"}
	for i, w := range want {
		if got[i].Label != w {
			t.Errorf("row %d = %q, want %q", i, got[i].Label, w)
		}
	}
}

func TestRankLimitsToTop(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session("a", "/p/a", transcript.Usage{Output: 3}),
		session("b", "/p/b", transcript.Usage{Output: 2}),
		session("c", "/p/c", transcript.Usage{Output: 1}),
	})

	if got := Rank(rows, 2); len(got) != 2 {
		t.Errorf("Rank(rows, 2) returned %d rows, want 2", len(got))
	}
	if got := Rank(rows, 0); len(got) != 3 {
		t.Errorf("Rank(rows, 0) returned %d rows, want all 3", len(got))
	}
	if got := Rank(rows, 99); len(got) != 3 {
		t.Errorf("Rank(rows, 99) returned %d rows, want all 3", len(got))
	}
}

// A label is raw user input: it can be long and contain newlines. Either would
// break the table if passed through untouched.
func TestTableKeepsColumnsAlignedWithAwkwardLabels(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session("short", "/home/u/proj", transcript.Usage{Output: 100}),
		session("multi\nline\tlabel that runs on and on and on and on and on and on", "/home/u/other", transcript.Usage{Output: 50}),
	})

	out := Table(Rank(rows, 0), false)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want header + 2 rows:\n%s", len(lines), out)
	}

	if strings.ContainsAny(strings.Join(lines[1:], ""), "\n\t") {
		t.Error("row text still contains newlines or tabs")
	}

	taskCol := strings.Index(lines[0], "TASK")
	for i, l := range lines[1:] {
		if len(l) < taskCol {
			t.Fatalf("row %d shorter than the TASK column start:\n%s", i, out)
		}
		if l[taskCol-1] != ' ' {
			t.Errorf("row %d is misaligned at the TASK column:\n%s", i, out)
		}
	}
}

func TestTableTruncatesLongLabels(t *testing.T) {
	long := strings.Repeat("x", 200)
	rows := Summarize([]transcript.Session{session(long, "/home/u/proj", transcript.Usage{Output: 1})})

	out := Table(Rank(rows, 0), false)
	for _, l := range strings.Split(out, "\n") {
		if len(l) > 160 {
			t.Errorf("line is %d chars, want a bounded width:\n%s", len(l), l)
		}
	}
}

func TestTableEmptyRowsPrintsNothing(t *testing.T) {
	if got := Table(nil, false); got != "" {
		t.Errorf("Table(nil, false) = %q, want empty", got)
	}
}

func TestShortProjectKeepsTheTail(t *testing.T) {
	cases := map[string]string{
		"/home/u/orca/workspaces/monolith/feat-payment": "monolith/feat-payment",
		"/home/u/project": "u/project",
		"proj":            "proj",
		"":                "",
	}
	for in, want := range cases {
		if got := shortProject(in); got != want {
			t.Errorf("shortProject(%q) = %q, want %q", in, got, want)
		}
	}
}

// --json is meant to be piped: it must be an array and nothing else.
func TestJSONHasNoHeaderOrFormatting(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session("multi\nline prompt", "/home/u/repo", transcript.Usage{Output: 1234}),
	})

	data, err := JSON(Rank(rows, 0))
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("output is not a JSON array: %v\n%s", err, data)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0]["task"] != "multi line prompt" {
		t.Errorf("task = %q, want newlines collapsed", got[0]["task"])
	}
	if got[0]["output_tokens"] != float64(1234) {
		t.Errorf("output_tokens = %v, want the exact count, not a formatted one", got[0]["output_tokens"])
	}
}

// Quota is omitted rather than reported as zero when there is no official
// percentage: a zero would read as a measurement.
func TestJSONOmitsQuotaWhenAbsent(t *testing.T) {
	rows := Summarize([]transcript.Session{session("a", "/home/u/repo", transcript.Usage{Output: 1})})

	data, err := JSON(rows)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if strings.Contains(string(data), "quota") {
		t.Errorf("quota present without an official percentage: %s", data)
	}
}

// Transcript text is untrusted and goes straight to a terminal: a pasted log or
// a fetched page can carry escape sequences that recolour the screen, move the
// cursor, or overwrite rows that were already printed.
func TestTableStripsTerminalControlSequences(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session("\x1b[31mFAKE ROW 99.9%\x07 real prompt", "/home/u/\x1b[2Jevil", transcript.Usage{Output: 1}),
	})

	out := Table(Rank(rows, 0), false)
	for _, bad := range []string{"\x1b", "\x07", "\x9b"} {
		if strings.Contains(out, bad) {
			t.Errorf("control byte %q reached the terminal:\n%q", bad, out)
		}
	}
	if !strings.Contains(out, "FAKE ROW 99.9% real prompt") {
		t.Errorf("stripping removed legitimate text:\n%s", out)
	}
	// Only the control byte goes; the printable remainder stays as plain text,
	// which is exactly what makes the row visibly odd instead of deceptive.
	if !strings.Contains(out, "u/[2Jevil") {
		t.Errorf("PROJECT column was mangled beyond the control byte:\n%s", out)
	}
}
