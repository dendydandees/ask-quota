package report

import (
	"strings"
	"testing"

	"github.com/dendydandees/ask-quota/internal/transcript"
)

// QUOTA is SHARE scaled by the official percentage: a row holding half the
// window's cost holds half of what the window has consumed.
func TestWithQuotaScalesShares(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session("a", "/p/a", transcript.Usage{Output: 30}),
		session("b", "/p/b", transcript.Usage{Output: 10}),
	})

	got := WithQuota(rows, 40)
	if want := 30.0; got[0].Quota != want {
		t.Errorf("Quota = %v, want %v (75%% of 40%%)", got[0].Quota, want)
	}
	if want := 10.0; got[1].Quota != want {
		t.Errorf("Quota = %v, want %v (25%% of 40%%)", got[1].Quota, want)
	}
}

func TestTableShowsQuotaColumnOnlyWhenAsked(t *testing.T) {
	rows := Summarize([]transcript.Session{session("a", "/p/a", transcript.Usage{Output: 1})})

	if got := Table(rows, false); strings.Contains(got, "QUOTA") {
		t.Error("QUOTA column shown without an official percentage")
	}
	if got := Table(WithQuota(rows, 40), true); !strings.Contains(got, "QUOTA") {
		t.Error("QUOTA column missing when an official percentage is available")
	}
}
