package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dendydandees/ask-quota/internal/cli"
)

// stubQuotaSource puts a quota source on PATH reporting the given percentage
// for the current 5-hour window.
func stubQuotaSource(t *testing.T, percentUsed float64) {
	t.Helper()

	bin := t.TempDir()
	// PATH holds only this directory, so the stub must not reach for a binary
	// of its own — echo is a shell builtin.
	script := fmt.Sprintf("#!/bin/sh\necho '"+
		`{"providers":[{"provider":"claude","windows":[{"id":"five_hour","percentUsed":%v,"resetsAt":%q}]}]}`+
		"'\n", percentUsed, time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	if err := os.WriteFile(filepath.Join(bin, "quota-axi"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

// Success Criterion #2: with an official source the report scales local shares
// into points of the real quota window, and without it the same corpus prints
// the same rows minus that column. Nothing else runs the official branch, so
// dropping WithQuota or passing showQuota=false would go unnoticed.
func TestRunShowsTheQuotaColumnWithAnOfficialSource(t *testing.T) {
	fakeHome(t, "fix the booking validation", 1234) // also empties PATH

	cfg, err := cli.Parse(nil)
	if err != nil {
		t.Fatal(err)
	}

	var without bytes.Buffer
	if err := run(cfg, &without); err != nil {
		t.Fatalf("run without source: %v", err)
	}

	stubQuotaSource(t, 40) // same HOME, now with a source
	var with bytes.Buffer
	if err := run(cfg, &with); err != nil {
		t.Fatalf("run with source: %v", err)
	}

	for _, want := range []string{"QUOTA", "official, 40% used", "fix the booking validation"} {
		if !strings.Contains(with.String(), want) {
			t.Errorf("report with a source is missing %q:\n%s", want, with.String())
		}
	}
	// The single session holds the whole window, so it accounts for all 40 points.
	if !strings.Contains(with.String(), "40.0%") {
		t.Errorf("QUOTA is not SHARE scaled by the official percentage:\n%s", with.String())
	}

	if strings.Contains(without.String(), "QUOTA") {
		t.Errorf("QUOTA column appeared without a source:\n%s", without.String())
	}
	if !strings.Contains(without.String(), "inferred") {
		t.Errorf("sourceless report does not mark the window inferred:\n%s", without.String())
	}
}
