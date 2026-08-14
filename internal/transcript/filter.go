package transcript

import "time"

// Times returns every message timestamp across sessions. Window inference
// needs the shape of recent activity, not the usage attached to it.
func Times(sessions []Session) []time.Time {
	var out []time.Time
	for _, s := range sessions {
		for _, m := range s.Messages {
			out = append(out, m.Time)
		}
	}
	return out
}

// Since drops messages before t, and sessions left with none. Scanning uses a
// wider lookback than the window itself, so this trims the surplus.
func Since(sessions []Session, t time.Time) []Session {
	out := make([]Session, 0, len(sessions))
	for _, s := range sessions {
		kept := make([]Message, 0, len(s.Messages))
		for _, m := range s.Messages {
			if !m.Time.Before(t) {
				kept = append(kept, m)
			}
		}
		if len(kept) > 0 {
			s.Messages = kept
			out = append(out, s)
		}
	}
	return out
}
