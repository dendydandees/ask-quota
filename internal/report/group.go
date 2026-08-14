package report

import "fmt"

// GroupByProject merges rows that share a working directory. One feature
// worked across several repos shows up as several small rows otherwise, which
// hides that it was the most expensive thing in the window.
func GroupByProject(rows []Row) []Row {
	if len(rows) == 0 {
		return nil
	}

	order := make([]string, 0, len(rows))
	byProject := make(map[string]*Row, len(rows))
	counts := make(map[string]int, len(rows))

	for _, r := range rows {
		g, ok := byProject[r.Project]
		if !ok {
			merged := Row{Project: r.Project}
			byProject[r.Project] = &merged
			order = append(order, r.Project)
			g = &merged
		}
		counts[r.Project]++
		g.Usage.Input += r.Usage.Input
		g.Usage.Output += r.Usage.Output
		g.Usage.CacheWrite += r.Usage.CacheWrite
		g.Usage.CacheRead += r.Usage.CacheRead
		g.Cost += r.Cost
		g.Share += r.Share
		g.Quota += r.Quota
	}

	out := make([]Row, 0, len(order))
	for _, p := range order {
		r := *byProject[p]
		// PROJECT already names the directory, so the label carries what the
		// merge hid instead: how many sessions went into this row.
		r.Label = fmt.Sprintf("%d sessions", counts[p])
		if counts[p] == 1 {
			r.Label = "1 session"
		}
		out = append(out, r)
	}
	return out
}
