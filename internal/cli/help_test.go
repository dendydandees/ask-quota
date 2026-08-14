package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/dendydandees/ask-quota/internal/window"
)

// Help is the first thing a stranger reads; it has to fit one screen.
func TestUsageFitsOneScreen(t *testing.T) {
	if n := strings.Count(Usage, "\n"); n > 40 {
		t.Errorf("--help is %d lines, want at most 40", n)
	}
}

func TestUsageNamesEveryFlagAndWindow(t *testing.T) {
	for _, want := range []string{"--window", "--top", "--group-by", "--json", "--version", "--help"} {
		if !strings.Contains(Usage, want) {
			t.Errorf("--help does not mention %q", want)
		}
	}
	for _, kind := range window.Kinds {
		if !strings.Contains(Usage, kind) {
			t.Errorf("--help does not mention the %q window", kind)
		}
	}
	for _, g := range groupings {
		if !strings.Contains(Usage, g) {
			t.Errorf("--help does not mention --group-by %q", g)
		}
	}
}

// Users must not read a partial number as a total, and must not read the
// QUOTA estimate as a measurement.
func TestUsageStatesTheLimitations(t *testing.T) {
	for _, want := range []string{"this machine", "not a", "measurement"} {
		if !strings.Contains(Usage, want) {
			t.Errorf("--help does not state %q", want)
		}
	}
}

// The man page and --help drift apart the moment one is edited alone.
func TestManPageCoversTheSameSurface(t *testing.T) {
	page, err := os.ReadFile("../../man/ask-quota.1")
	if err != nil {
		t.Fatalf("man page missing: %v", err)
	}
	// troff writes a literal hyphen as \-, so undo that before comparing.
	man := strings.ReplaceAll(string(page), `\-`, "-")

	for _, want := range []string{"--window", "--top", "--group-by", "--json", "--version"} {
		if !strings.Contains(man, want) {
			t.Errorf("man page does not document %q", want)
		}
	}
	for _, section := range []string{".SH NAME", ".SH SYNOPSIS", ".SH DESCRIPTION", ".SH OPTIONS", ".SH EXAMPLES", ".SH LIMITATIONS"} {
		if !strings.Contains(man, section) {
			t.Errorf("man page is missing %s", section)
		}
	}
	if !strings.Contains(man, "this machine") {
		t.Error("man page does not state the local-only limitation")
	}
}

// Defaults stated in the docs must be the defaults the parser applies.
func TestDocumentedDefaultsMatchTheParser(t *testing.T) {
	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !strings.Contains(Usage, "(default 5)") || cfg.Top != 5 {
		t.Errorf("--top default is %d but help says otherwise", cfg.Top)
	}
	if !strings.Contains(Usage, "5h       the current 5-hour block (default)") || cfg.Window != window.Session {
		t.Errorf("window default is %q but help says otherwise", cfg.Window)
	}
}
