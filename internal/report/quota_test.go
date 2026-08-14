package report

import (
	"strings"
	"testing"

	"github.com/dendy-fisiohome/ask-quota/internal/transcript"
	"github.com/dendy-fisiohome/ask-quota/internal/window"
)

const sample = `{"providers":[{"provider":"claude","windows":[
  {"id":"five_hour","percentUsed":51,"resetsAt":"2026-08-14T09:20:00.388297+00:00"},
  {"id":"seven_day","percentUsed":57,"resetsAt":"2026-08-16T05:59:59.906805+00:00"},
  {"id":"model:fable","percentUsed":7,"resetsAt":"2026-08-16T05:59:59.907168+00:00"}]}]}`

func TestParseOfficialReadsTheMatchingWindow(t *testing.T) {
	got, ok := parseOfficial([]byte(sample), window.Session)
	if !ok {
		t.Fatal("want the five_hour window to be found")
	}
	if got.PercentUsed != 51 {
		t.Errorf("PercentUsed = %v, want 51", got.PercentUsed)
	}
	if got.ResetsAt.IsZero() {
		t.Error("ResetsAt was not parsed")
	}

	week, ok := parseOfficial([]byte(sample), window.Week)
	if !ok || week.PercentUsed != 57 {
		t.Errorf("week = %+v, ok = %v, want 57", week, ok)
	}
}

// No 30-day window exists upstream, so there is nothing to match.
func TestParseOfficialHasNoMonthWindow(t *testing.T) {
	if _, ok := parseOfficial([]byte(sample), window.Month); ok {
		t.Error("want no official window for 30d")
	}
}

func TestParseOfficialToleratesJunk(t *testing.T) {
	for _, in := range []string{"", "not json", `{"providers":[]}`, `{"providers":[{"windows":[]}]}`} {
		if _, ok := parseOfficial([]byte(in), window.Session); ok {
			t.Errorf("parseOfficial(%q) reported success, want a clean miss", in)
		}
	}
}

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
