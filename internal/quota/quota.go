// Package quota reads the optional external view of a quota window.
package quota

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"

	"github.com/dendy-fisiohome/ask-quota/internal/window"
)

// Official is the upstream view of a quota window: when it resets and how much
// of it has been consumed.
type Official struct {
	ResetsAt    time.Time
	PercentUsed float64
}

// officialWindowID maps a report window to the id upstream uses for it. The
// 30-day window is absent on purpose: no such window exists upstream.
var officialWindowID = map[string]string{
	window.Session: "five_hour",
	window.Week:    "seven_day",
}

// quotaSource is an external command that reports quota state as JSON. It is
// entirely optional — without it the tool still ranks sessions, it just cannot
// scale them to the real window.
var quotaSource = []string{"quota-axi", "--provider", "claude", "--json"}

// Lookup asks the optional quota source about a window. A missing,
// failing or slow source is simply absent: it must never turn into an error
// the user has to deal with.
func Lookup(kind string) (Official, bool) {
	if _, ok := officialWindowID[kind]; !ok {
		return Official{}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, quotaSource[0], quotaSource[1:]...).Output()
	if err != nil {
		return Official{}, false
	}
	return parse(out, kind)
}

func parse(data []byte, kind string) (Official, bool) {
	id, ok := officialWindowID[kind]
	if !ok {
		return Official{}, false
	}

	var payload struct {
		Providers []struct {
			Windows []struct {
				ID          string  `json:"id"`
				PercentUsed float64 `json:"percentUsed"`
				ResetsAt    string  `json:"resetsAt"`
			} `json:"windows"`
		} `json:"providers"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return Official{}, false
	}

	for _, p := range payload.Providers {
		for _, w := range p.Windows {
			if w.ID != id {
				continue
			}
			resets, err := time.Parse(time.RFC3339, w.ResetsAt)
			if err != nil {
				return Official{}, false
			}
			return Official{ResetsAt: resets, PercentUsed: w.PercentUsed}, true
		}
	}
	return Official{}, false
}
