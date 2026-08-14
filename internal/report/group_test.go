package report

import (
	"math"
	"testing"

	"github.com/dendy-fisiohome/ask-quota/internal/transcript"
)

func TestGroupByProjectMergesSessionsSharingADirectory(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session("first prompt", "/home/u/repo", transcript.Usage{Output: 10}),
		session("second prompt", "/home/u/repo", transcript.Usage{Output: 30}),
		session("elsewhere", "/home/u/other", transcript.Usage{Output: 10}),
	})

	got := GroupByProject(rows)
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}

	var merged Row
	for _, r := range got {
		if r.Project == "/home/u/repo" {
			merged = r
		}
	}
	if merged.Usage.Output != 40 {
		t.Errorf("Output = %d, want 40", merged.Usage.Output)
	}
	if want := 200.0; merged.Cost != want {
		t.Errorf("Cost = %v, want %v", merged.Cost, want)
	}
}

func TestGroupByProjectPreservesTotalShare(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session("a", "/home/u/repo", transcript.Usage{Output: 10}),
		session("b", "/home/u/repo", transcript.Usage{Output: 30}),
		session("c", "/home/u/other", transcript.Usage{Output: 10}),
	})

	var before float64
	for _, r := range rows {
		before += r.Share
	}
	var after float64
	for _, r := range GroupByProject(rows) {
		after += r.Share
	}
	if math.Abs(before-after) > 1e-9 || math.Abs(after-100) > 1e-9 {
		t.Errorf("shares changed under grouping: %v then %v, want 100 both", before, after)
	}
}

// PROJECT already names the directory, so the label reports what the merge
// hid: how many sessions the row stands for.
func TestGroupByProjectLabelsWithSessionCount(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session("first", "/home/u/repo", transcript.Usage{Output: 1}),
		session("second", "/home/u/repo", transcript.Usage{Output: 1}),
		session("alone", "/home/u/other", transcript.Usage{Output: 1}),
	})

	got := GroupByProject(rows)
	labels := map[string]string{}
	for _, r := range got {
		labels[r.Project] = r.Label
	}
	if want := "2 sessions"; labels["/home/u/repo"] != want {
		t.Errorf("Label = %q, want %q", labels["/home/u/repo"], want)
	}
	if want := "1 session"; labels["/home/u/other"] != want {
		t.Errorf("Label = %q, want %q", labels["/home/u/other"], want)
	}
}

func TestGroupByProjectCarriesQuotaThrough(t *testing.T) {
	rows := WithQuota(Summarize([]transcript.Session{
		session("a", "/home/u/repo", transcript.Usage{Output: 10}),
		session("b", "/home/u/repo", transcript.Usage{Output: 30}),
	}), 50)

	got := GroupByProject(rows)
	if want := 50.0; math.Abs(got[0].Quota-want) > 1e-9 {
		t.Errorf("Quota = %v, want %v", got[0].Quota, want)
	}
}

func TestGroupByProjectEmpty(t *testing.T) {
	if got := GroupByProject(nil); len(got) != 0 {
		t.Errorf("got %d rows, want 0", len(got))
	}
}
