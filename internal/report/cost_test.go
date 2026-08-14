package report

import (
	"math"
	"testing"

	"github.com/dendydandees/ask-quota/internal/transcript"
)

func TestCostAppliesPublishedWeights(t *testing.T) {
	cases := []struct {
		name string
		u    transcript.Usage
		want float64
	}{
		{"input only", transcript.Usage{Input: 100}, 100},
		{"output weighs 5x", transcript.Usage{Output: 100}, 500},
		{"cache write weighs 1.25x", transcript.Usage{CacheWrite: 100}, 125},
		{"cache read weighs 0.1x", transcript.Usage{CacheRead: 100}, 10},
		{"combined", transcript.Usage{Input: 10, Output: 10, CacheWrite: 10, CacheRead: 10}, 10 + 50 + 12.5 + 1},
		{"zero", transcript.Usage{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Cost(tc.u); math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("Cost(%+v) = %v, want %v", tc.u, got, tc.want)
			}
		})
	}
}

func session(label, cwd string, usages ...transcript.Usage) transcript.Session {
	s := transcript.Session{Label: label, CWD: cwd, File: label + ".jsonl"}
	for _, u := range usages {
		s.Messages = append(s.Messages, transcript.Message{Usage: u})
	}
	return s
}

func TestSummarizeSumsMessagesPerSession(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session("a", "/p/a", transcript.Usage{Output: 10}, transcript.Usage{Output: 30}),
	})

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Usage.Output != 40 {
		t.Errorf("Output = %d, want 40", rows[0].Usage.Output)
	}
	if want := 200.0; rows[0].Cost != want {
		t.Errorf("Cost = %v, want %v", rows[0].Cost, want)
	}
}

func TestSummarizeSharesSumTo100(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session("a", "/p/a", transcript.Usage{Output: 30}),
		session("b", "/p/b", transcript.Usage{Output: 10}),
	})

	var total float64
	for _, r := range rows {
		total += r.Share
	}
	if math.Abs(total-100) > 1e-9 {
		t.Errorf("shares sum to %v, want 100", total)
	}

	byLabel := map[string]float64{}
	for _, r := range rows {
		byLabel[r.Label] = r.Share
	}
	if math.Abs(byLabel["a"]-75) > 1e-9 {
		t.Errorf("share of a = %v, want 75", byLabel["a"])
	}
}

func TestSummarizeEmptyInputDoesNotDivideByZero(t *testing.T) {
	if rows := Summarize(nil); len(rows) != 0 {
		t.Errorf("got %d rows, want 0", len(rows))
	}
}

func TestSummarizeZeroCostDoesNotDivideByZero(t *testing.T) {
	rows := Summarize([]transcript.Session{session("a", "/p/a", transcript.Usage{})})

	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Share != 0 {
		t.Errorf("Share = %v, want 0 for a zero-cost corpus", rows[0].Share)
	}
}
