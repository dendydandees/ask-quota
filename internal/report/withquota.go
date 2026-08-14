package report

// WithQuota fills in each row's estimated share of the real quota window.
// The percentage comes from the caller; this package never asks anyone for it.
func WithQuota(rows []Row, percentUsed float64) []Row {
	out := make([]Row, len(rows))
	copy(out, rows)
	for i := range out {
		out[i].Quota = out[i].Share * percentUsed / 100
	}
	return out
}
