package report

import "testing"

// Every row in every report passes through this, so a wrong branch is a wrong
// number on screen for every user.
func TestTokensFormatsEachMagnitude(t *testing.T) {
	cases := map[int64]string{
		0:           "0",
		1:           "1",
		999:         "999",
		1_000:       "1k",
		1_500:       "1k",
		999_999:     "999k",
		1_000_000:   "1.0M",
		1_450_000:   "1.4M",
		228_314_458: "228.3M",
	}
	for in, want := range cases {
		if got := tokens(in); got != want {
			t.Errorf("tokens(%d) = %q, want %q", in, got, want)
		}
	}
}
