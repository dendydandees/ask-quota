// Package report turns scanned transcripts into ranked, printable rows.
package report

import (
	"strings"

	"github.com/dendydandees/ask-quota/internal/transcript"
)

// Weights are relative to an input token, following published price ratios.
// Deliberately model-agnostic: this ranks sessions, it does not price them.
const (
	weightInput      = 1.0
	weightCacheWrite = 1.25
	weightCacheRead  = 0.10
	weightOutput     = 5.0
)

// Row is one line of the report: a session or project with its totals.
type Row struct {
	Label   string
	Project string
	File    string
	Usage   transcript.Usage
	Cost    float64
	// Share is this row's percentage of the window's total cost.
	Share float64
	// Quota is Share scaled by the official window percentage: an estimate of
	// how many points of the real quota window this row accounts for. Zero
	// when no official percentage is available.
	Quota float64
}

// Cost returns cost units for a usage record.
func Cost(u transcript.Usage) float64 {
	return float64(u.Input)*weightInput +
		float64(u.Output)*weightOutput +
		float64(u.CacheWrite)*weightCacheWrite +
		float64(u.CacheRead)*weightCacheRead
}

// sanitize removes characters that let transcript text misrepresent itself on
// screen: C0/C1 controls and DEL, which move the cursor, erase lines and repaint
// rows already read; and the bidirectional overrides, which reorder a label so a
// row can name a project it did not come from.
//
// It runs here, where a session becomes a row, rather than in a renderer: the
// table escaped it but --json did not, and `--json | jq` decodes the escaping
// straight back into raw bytes on the way to the terminal. One boundary means a
// third output format cannot reopen the hole.
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n', r == '\r', r == '\t':
			// Whitespace controls become spaces so words stay separated; the
			// callers collapse runs of them.
			return ' '
		case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
			return -1
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069, r == 0x200e, r == 0x200f:
			return -1
		}
		return r
	}, s)
}

// Summarize collapses each session into a row and fills in each row's share of
// the total.
func Summarize(sessions []transcript.Session) []Row {
	rows := make([]Row, 0, len(sessions))
	var total float64

	for _, s := range sessions {
		var u transcript.Usage
		for _, m := range s.Messages {
			u = u.Plus(m.Usage)
		}
		c := Cost(u)
		total += c
		rows = append(rows, Row{
			Label:   sanitize(s.Label),
			Project: sanitize(s.CWD),
			File:    sanitize(s.File),
			Usage:   u,
			Cost:    c,
		})
	}

	// A corpus can legitimately total zero cost; shares stay zero rather than NaN.
	if total > 0 {
		for i := range rows {
			rows[i].Share = rows[i].Cost / total * 100
		}
	}
	return rows
}
