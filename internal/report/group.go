package report

import "fmt"

// GroupByProject merges rows that share a working directory. One feature
// worked across several repos shows up as several small rows otherwise, which
// hides that it was the most expensive thing in the window.
func GroupByProject(rows []Row) []Row {
	type group struct {
		row      Row
		sessions int
	}

	order := make([]string, 0, len(rows))
	byProject := make(map[string]*group, len(rows))

	for _, r := range rows {
		g := byProject[r.Project]
		if g == nil {
			g = &group{row: Row{Project: r.Project}}
			byProject[r.Project] = g
			order = append(order, r.Project)
		}
		g.sessions++
		g.row.Usage = g.row.Usage.Plus(r.Usage)
		g.row.Cost += r.Cost
		g.row.Share += r.Share
		g.row.Quota += r.Quota
	}

	out := make([]Row, 0, len(order))
	for _, p := range order {
		g := byProject[p]
		// PROJECT already names the directory, so the label carries what the
		// merge hid instead: how many sessions went into this row.
		g.row.Label = "1 session"
		if g.sessions > 1 {
			g.row.Label = fmt.Sprintf("%d sessions", g.sessions)
		}
		out = append(out, g.row)
	}
	return out
}
