package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/dendydandees/ask-quota/internal/cli"
	"github.com/dendydandees/ask-quota/internal/quota"
)

// stubOfficial drives the official branch without a token or a network. The
// seam is main's own lookupOfficial rather than anything inside the quota
// package: an override there would have to name the host the bearer token is
// sent to, and that is not a knob worth having.
func stubOfficial(t *testing.T, percentUsed float64) {
	t.Helper()

	original := lookupOfficial
	t.Cleanup(func() { lookupOfficial = original })
	lookupOfficial = func(string) (quota.Official, string) {
		return quota.Official{
			ResetsAt:    time.Now().Add(time.Hour),
			PercentUsed: percentUsed,
		}, ""
	}
}

// Success Criterion #2: with an official figure the report scales local shares
// into points of the real quota window, and without it the same corpus prints
// the same rows minus that column. Nothing else runs the official branch, so
// dropping WithQuota or passing showQuota=false would go unnoticed.
func TestRunShowsTheQuotaColumnWithAnOfficialSource(t *testing.T) {
	fakeHome(t, "fix the booking validation", 1234)

	cfg, err := cli.Parse(nil)
	if err != nil {
		t.Fatal(err)
	}

	var without bytes.Buffer
	if err := run(cfg, &without, io.Discard); err != nil {
		t.Fatalf("run without an official figure: %v", err)
	}

	stubOfficial(t, 40) // same corpus, now with one
	var with bytes.Buffer
	if err := run(cfg, &with, io.Discard); err != nil {
		t.Fatalf("run with an official figure: %v", err)
	}

	for _, want := range []string{"QUOTA", "official, 40% used", "fix the booking validation"} {
		if !strings.Contains(with.String(), want) {
			t.Errorf("report with an official figure is missing %q:\n%s", want, with.String())
		}
	}
	// The single session holds the whole window, so it accounts for all 40 points.
	if !strings.Contains(with.String(), "40.0%") {
		t.Errorf("QUOTA is not SHARE scaled by the official percentage:\n%s", with.String())
	}

	if strings.Contains(without.String(), "QUOTA") {
		t.Errorf("QUOTA column appeared without an official figure:\n%s", without.String())
	}
	if !strings.Contains(without.String(), "inferred") {
		t.Errorf("report without an official figure does not mark the window inferred:\n%s", without.String())
	}
}
