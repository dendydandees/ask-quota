// Package report turns scanned transcripts into ranked, printable rows.
package report

import "github.com/dendy-fisiohome/ask-quota/internal/transcript"

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

// Summarize collapses each session into a row and fills in each row's share of
// the total.
func Summarize(sessions []transcript.Session) []Row {
	rows := make([]Row, 0, len(sessions))
	var total float64

	for _, s := range sessions {
		var u transcript.Usage
		for _, m := range s.Messages {
			u.Input += m.Usage.Input
			u.Output += m.Usage.Output
			u.CacheWrite += m.Usage.CacheWrite
			u.CacheRead += m.Usage.CacheRead
		}
		c := Cost(u)
		total += c
		rows = append(rows, Row{
			Label:   s.Label,
			Project: s.CWD,
			File:    s.File,
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
