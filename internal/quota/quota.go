// Package quota reads the optional external view of a quota window.
package quota

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"

	"github.com/dendydandees/ask-quota/internal/window"
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

// lookupTimeout is generous because the lookup runs alongside the scan, so its
// latency costs no wall clock. A variable so tests need not wait it out.
var lookupTimeout = 8 * time.Second

// maxOutput bounds what the source may print, ~1000x the real payload.
const maxOutput = 1 << 20

// cappedBuffer collects at most maxOutput bytes and silently discards the rest,
// so an endless source neither grows memory nor stalls the writer.
type cappedBuffer struct{ data []byte }

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := maxOutput - len(b.data); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		b.data = append(b.data, p[:room]...)
	}
	return len(p), nil
}

// Lookup asks the optional quota source about a window. A missing,
// failing or slow source is simply absent: it must never turn into an error
// the user has to deal with.
func Lookup(kind string) (Official, bool) {
	if _, ok := officialWindowID[kind]; !ok {
		return Official{}, false
	}

	ctx, cancel := context.WithTimeout(context.Background(), lookupTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, quotaSource[0], quotaSource[1:]...)
	// The context kills the child, but Wait blocks until every writer of the
	// stdout pipe closes it — a source that forks (any shell wrapper does) would
	// otherwise hang this call, and with it the whole program.
	cmd.WaitDelay = time.Second

	// Writing into a capped buffer rather than reading a StdoutPipe is what
	// makes WaitDelay effective: os/exec then owns the pipe and can force it
	// closed. It also bounds memory — the real payload is a few hundred bytes,
	// and a runaway source must not fill memory before the timeout fires.
	var buf cappedBuffer
	cmd.Stdout = &buf
	if err := cmd.Run(); err != nil {
		return Official{}, false
	}
	return parse(buf.data, kind)
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
