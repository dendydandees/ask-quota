// Command ask-quota reports which tasks consumed your Claude Code quota,
// read from local transcripts.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/dendy-fisiohome/ask-quota/internal/cli"
	"github.com/dendy-fisiohome/ask-quota/internal/quota"
	"github.com/dendy-fisiohome/ask-quota/internal/report"
	"github.com/dendy-fisiohome/ask-quota/internal/transcript"
	"github.com/dendy-fisiohome/ask-quota/internal/window"
)

// version is overridden at build time with -ldflags="-X main.version=...".
var version = "dev"

func main() {
	cfg, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ask-quota:", err)
		os.Exit(2)
	}

	switch cfg.Intent {
	case cli.IntentHelp:
		fmt.Print(cli.Usage)
		return
	case cli.IntentVersion:
		fmt.Printf("ask-quota %s\n", version)
		return
	}

	if err := run(cfg, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "ask-quota:", err)
		os.Exit(2)
	}
}

func run(cfg cli.Config, w io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root := filepath.Join(home, ".claude", "projects")
	now := time.Now()

	// The quota source is optional: without it the window is inferred and the
	// QUOTA column is dropped, but nothing else changes. It runs alongside the
	// scan so a slow lookup costs no wall clock — and so its timeout can be
	// generous enough not to degrade on a cold start.
	pending := make(chan quota.Official, 1)
	go func() {
		o, _ := quota.Lookup(cfg.Window)
		pending <- o
	}()

	// Scanned wider than the window: a session block is defined by the idle
	// gap before it, which is only visible from earlier activity.
	scanned, err := transcript.Scan(root, window.Lookback(cfg.Window, now))
	if err != nil {
		return err
	}

	// An absent source is the zero Official, which is exactly what the window
	// resolver already treats as "no official boundary".
	official := <-pending
	haveOfficial := !official.ResetsAt.IsZero()

	// Activity timestamps are only consulted to infer a session block, which
	// happens for one window and only without an official reset. Collecting
	// them otherwise would allocate one entry per message across the corpus
	// for a value the resolver never reads.
	var times []time.Time
	if cfg.Window == window.Session && !haveOfficial {
		times = transcript.Times(scanned)
	}

	start, err := window.Resolve(cfg.Window, now, official.ResetsAt, times)
	if err != nil {
		return err
	}

	sessions := transcript.Since(scanned, start)
	if len(sessions) == 0 {
		fmt.Fprintf(w, "No Claude Code usage found in this %s window (since %s).\n",
			cfg.Window, start.Local().Format(time.RFC3339))
		return nil
	}

	rows := report.Summarize(sessions)
	if haveOfficial {
		rows = report.WithQuota(rows, official.PercentUsed)
	}
	if cfg.GroupBy == cli.GroupProject {
		rows = report.GroupByProject(rows)
	}
	rows = report.Rank(rows, cfg.Top)

	if cfg.JSON {
		data, err := report.JSON(rows)
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(data))
		return nil
	}

	fmt.Fprintln(w, windowLine(cfg.Window, start, official, haveOfficial))
	fmt.Fprint(w, report.Table(rows, haveOfficial))
	return nil
}

// windowLine says which window the numbers cover and whether its boundary came
// from the quota source or was inferred locally.
func windowLine(kind string, start time.Time, official quota.Official, haveOfficial bool) string {
	origin := "inferred"
	used := ""
	if haveOfficial {
		origin = "official"
		used = fmt.Sprintf(", %.0f%% used", official.PercentUsed)
	}
	return fmt.Sprintf("window %s: since %s (%s%s)",
		kind, start.Local().Format(time.RFC3339), origin, used)
}
