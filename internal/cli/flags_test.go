package cli

import (
	"strings"
	"testing"

	"github.com/dendy-fisiohome/ask-quota/internal/window"
)

func TestParseDefaults(t *testing.T) {
	got, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Window != window.Session {
		t.Errorf("Window = %q, want %q", got.Window, window.Session)
	}
	if got.Top != 5 {
		t.Errorf("Top = %d, want 5", got.Top)
	}
	if got.GroupBy != GroupSession {
		t.Errorf("GroupBy = %q, want %q", got.GroupBy, GroupSession)
	}
	if got.JSON {
		t.Error("JSON = true, want false")
	}
}

func TestParseAcceptsValidInput(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want Config
	}{
		{"positional window", []string{"week"}, Config{Window: window.Week, Top: 5, GroupBy: GroupSession}},
		{"window flag", []string{"--window", "30d"}, Config{Window: window.Month, Top: 5, GroupBy: GroupSession}},
		{"top", []string{"--top", "10"}, Config{Window: window.Session, Top: 10, GroupBy: GroupSession}},
		{"top all", []string{"--top", "all"}, Config{Window: window.Session, Top: 0, GroupBy: GroupSession}},
		{"group by project", []string{"--group-by", "project"}, Config{Window: window.Session, Top: 5, GroupBy: GroupProject}},
		{"json", []string{"--json"}, Config{Window: window.Session, Top: 5, GroupBy: GroupSession, JSON: true}},
		{"combined", []string{"30d", "--top", "all", "--group-by", "project"}, Config{Window: window.Month, Top: 0, GroupBy: GroupProject}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.args)
			if err != nil {
				t.Fatalf("Parse(%v): %v", tc.args, err)
			}
			if got != tc.want {
				t.Errorf("Parse(%v) = %+v, want %+v", tc.args, got, tc.want)
			}
		})
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantHint string
	}{
		{"unknown window", []string{"9y"}, "9y"},
		{"unknown window flag", []string{"--window", "yearly"}, "yearly"},
		{"zero top", []string{"--top", "0"}, "top"},
		{"negative top", []string{"--top", "-3"}, "top"},
		{"non-numeric top", []string{"--top", "many"}, "top"},
		{"unknown group", []string{"--group-by", "repo"}, "repo"},
		{"extra positional", []string{"week", "extra"}, "extra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.args)
			if err == nil {
				t.Fatalf("Parse(%v) succeeded, want an error", tc.args)
			}
			if !strings.Contains(err.Error(), tc.wantHint) {
				t.Errorf("error %q does not mention %q", err, tc.wantHint)
			}
		})
	}
}

// An error message that does not say what is allowed makes the user go
// hunting for --help.
func TestParseErrorsListValidValues(t *testing.T) {
	_, err := Parse([]string{"--window", "yearly"})
	if err == nil {
		t.Fatal("want an error")
	}
	for _, kind := range window.Kinds {
		if !strings.Contains(err.Error(), kind) {
			t.Errorf("error %q does not list %q", err, kind)
		}
	}
}
