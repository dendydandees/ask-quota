package transcript

import (
	"testing"
	"time"
)

func at(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad test timestamp %q: %v", s, err)
	}
	return v
}

func TestTimesCollectsEveryMessage(t *testing.T) {
	sessions := []Session{
		{Messages: []Message{{Time: at(t, "2026-08-14T01:00:00Z")}, {Time: at(t, "2026-08-14T02:00:00Z")}}},
		{Messages: []Message{{Time: at(t, "2026-08-14T03:00:00Z")}}},
	}
	if got := Times(sessions); len(got) != 3 {
		t.Errorf("got %d timestamps, want 3", len(got))
	}
}

func TestSinceTrimsMessagesAndEmptySessions(t *testing.T) {
	sessions := []Session{
		{CWD: "keeps", Messages: []Message{
			{ID: "old", Time: at(t, "2026-08-14T01:00:00Z")},
			{ID: "new", Time: at(t, "2026-08-14T05:00:00Z")},
		}},
		{CWD: "drops", Messages: []Message{
			{ID: "old", Time: at(t, "2026-08-14T02:00:00Z")},
		}},
	}

	got := Since(sessions, at(t, "2026-08-14T04:00:00Z"))
	if len(got) != 1 {
		t.Fatalf("got %d sessions, want 1", len(got))
	}
	if got[0].CWD != "keeps" {
		t.Errorf("kept %q, want the session with an in-window message", got[0].CWD)
	}
	if len(got[0].Messages) != 1 || got[0].Messages[0].ID != "new" {
		t.Errorf("got %+v, want only the in-window message", got[0].Messages)
	}
}

// The boundary instant belongs to the window.
func TestSinceIncludesTheBoundary(t *testing.T) {
	boundary := at(t, "2026-08-14T04:00:00Z")
	sessions := []Session{{Messages: []Message{{Time: boundary}}}}

	if got := Since(sessions, boundary); len(got) != 1 {
		t.Error("a message exactly at the boundary must be kept")
	}
}
