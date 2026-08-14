package report

import (
	"testing"

	"github.com/dendy-fisiohome/ask-quota/internal/window"
)

// The quota source is optional by design. Every way it can go wrong must read
// as "absent", never as an error the user has to deal with.
func TestLookupOfficialTreatsEveryFailureAsAbsent(t *testing.T) {
	original := quotaSource
	t.Cleanup(func() { quotaSource = original })

	cases := []struct {
		name    string
		command []string
	}{
		{"binary not installed", []string{"ask-quota-no-such-command-exists"}},
		{"source exits non-zero", []string{"false"}},
		{"source prints junk", []string{"echo", "not json"}},
		{"source prints an unrelated window", []string{"echo", `{"providers":[{"windows":[{"id":"model:fable","percentUsed":7}]}]}`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			quotaSource = tc.command
			if _, ok := LookupOfficial(window.Session); ok {
				t.Error("reported success, want a clean absence")
			}
		})
	}
}

func TestLookupOfficialReadsAWorkingSource(t *testing.T) {
	original := quotaSource
	t.Cleanup(func() { quotaSource = original })

	quotaSource = []string{"echo", sample}
	got, ok := LookupOfficial(window.Session)
	if !ok {
		t.Fatal("want the window to be found")
	}
	if got.PercentUsed != 51 {
		t.Errorf("PercentUsed = %v, want 51", got.PercentUsed)
	}
}

// There is no 30-day window upstream, so the source is never even consulted.
func TestLookupOfficialSkipsTheSourceForMonth(t *testing.T) {
	original := quotaSource
	t.Cleanup(func() { quotaSource = original })

	quotaSource = []string{"ask-quota-this-would-fail-if-run"}
	if _, ok := LookupOfficial(window.Month); ok {
		t.Error("want no official window for 30d")
	}
}
