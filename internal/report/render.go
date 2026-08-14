package report

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"
)

// labelWidth bounds the TASK column. Labels are raw user prompts and can be
// arbitrarily long.
const labelWidth = 55

// Rank sorts rows by cost, highest first, and keeps at most top of them.
// A top of zero or less keeps every row.
func Rank(rows []Row, top int) []Row {
	sorted := make([]Row, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Cost > sorted[j].Cost })

	if top > 0 && top < len(sorted) {
		sorted = sorted[:top]
	}
	return sorted
}

// Table renders rows as an aligned text table. It returns "" for no rows.
// The QUOTA column appears only when showQuota is set, since without an
// official percentage there is nothing honest to put in it.
func Table(rows []Row, showQuota bool) string {
	if len(rows) == 0 {
		return ""
	}

	// Header and rows are built in the same shape, so the optional column
	// cannot drift out of position between them.
	header := []string{"SHARE"}
	if showQuota {
		header = append(header, "QUOTA")
	}
	header = append(header, "OUT", "CACHE_W", "CACHE_R", "PROJECT", "TASK")

	grid := [][]string{header}
	for _, r := range rows {
		cells := []string{percent(r.Share)}
		if showQuota {
			cells = append(cells, percent(r.Quota))
		}
		grid = append(grid, append(cells,
			tokens(r.Usage.Output),
			tokens(r.Usage.CacheWrite),
			tokens(r.Usage.CacheRead),
			shortProject(r.Project),
			clean(r.Label),
		))
	}

	widths := make([]int, len(grid[0]))
	for _, row := range grid {
		for i, cell := range row {
			if n := utf8.RuneCountInString(cell); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder
	for _, row := range grid {
		for i, cell := range row {
			b.WriteString(cell)
			// The last column is ragged: no trailing padding.
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-utf8.RuneCountInString(cell)+2))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// JSON renders rows for piping. It carries no header and no formatting, so
// the output is consumable as-is.
func JSON(rows []Row) ([]byte, error) {
	type jsonRow struct {
		Share      float64 `json:"share"`
		Quota      float64 `json:"quota,omitempty"`
		Project    string  `json:"project"`
		Task       string  `json:"task"`
		File       string  `json:"file"`
		Input      int64   `json:"input_tokens"`
		Output     int64   `json:"output_tokens"`
		CacheWrite int64   `json:"cache_write_tokens"`
		CacheRead  int64   `json:"cache_read_tokens"`
	}

	out := make([]jsonRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, jsonRow{
			Share:      r.Share,
			Quota:      r.Quota,
			Project:    r.Project,
			Task:       strings.Join(strings.Fields(r.Label), " "),
			File:       r.File,
			Input:      r.Usage.Input,
			Output:     r.Usage.Output,
			CacheWrite: r.Usage.CacheWrite,
			CacheRead:  r.Usage.CacheRead,
		})
	}
	return json.Marshal(out)
}

func percent(v float64) string { return fmt.Sprintf("%.1f%%", v) }

// tokens formats a token count compactly; exact counts do not aid comparison.
func tokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// shortProject keeps the last two path segments, which is where the repo and
// branch live; the leading home path is the same for every row.
func shortProject(p string) string {
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return ""
	}
	dir, base := path.Split(p)
	parent := path.Base(strings.TrimSuffix(dir, "/"))
	if dir == "" || parent == "." || parent == "/" {
		return base
	}
	return parent + "/" + base
}

// clean flattens a raw user prompt into one bounded single-line cell.
func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > labelWidth {
		return string([]rune(s)[:labelWidth-1]) + "…"
	}
	return s
}
