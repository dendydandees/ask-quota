package report

import (
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

	out := Table(Rank(rows, 0))
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

	out := Table(Rank(rows, 0))
	for _, l := range strings.Split(out, "\n") {
		if len(l) > 160 {
			t.Errorf("line is %d chars, want a bounded width:\n%s", len(l), l)
		}
	}
}

func TestTableEmptyRowsPrintsNothing(t *testing.T) {
	if got := Table(nil); got != "" {
		t.Errorf("Table(nil) = %q, want empty", got)
	}
}

func TestShortProjectKeepsTheTail(t *testing.T) {
	cases := map[string]string{
		"/home/u/orca/workspaces/monolith/feat-payment": "monolith/feat-payment",
		"/home/u/project":                               "u/project",
		"proj":                                          "proj",
		"":                                              "",
	}
	for in, want := range cases {
		if got := shortProject(in); got != want {
			t.Errorf("shortProject(%q) = %q, want %q", in, got, want)
		}
	}
}
