// Command ask-quota reports which tasks consumed your Claude Code quota,
// read from local transcripts.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dendy-fisiohome/ask-quota/internal/cli"
	"github.com/dendy-fisiohome/ask-quota/internal/report"
	"github.com/dendy-fisiohome/ask-quota/internal/transcript"
	"github.com/dendy-fisiohome/ask-quota/internal/window"
)

// version is overridden at build time with -ldflags="-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v":
			fmt.Printf("ask-quota %s\n", version)
			return
		case "--help", "-h", "help":
			fmt.Print(cli.Usage)
			return
		}
	}

	cfg, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ask-quota:", err)
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "ask-quota:", err)
		os.Exit(2)
	}
}

func run(cfg cli.Config) error {
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
	type lookup struct {
		official report.Official
		ok       bool
	}
	quota := make(chan lookup, 1)
	go func() {
		o, ok := report.LookupOfficial(cfg.Window)
		quota <- lookup{o, ok}
	}()

	// Scanned wider than the window: a session block is defined by the idle
	// gap before it, which is only visible from earlier activity.
	scanned, err := transcript.Scan(root, window.Lookback(cfg.Window, now))
	if err != nil {
		return err
	}

	got := <-quota
	official, haveOfficial := got.official, got.ok

	start, err := window.Resolve(cfg.Window, now, official.ResetsAt, transcript.Times(scanned))
	if err != nil {
		return err
	}

	sessions := transcript.Since(scanned, start)
	if len(sessions) == 0 {
		fmt.Printf("No Claude Code usage found in this %s window (since %s).\n",
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
		fmt.Println(string(data))
		return nil
	}

	fmt.Println(windowLine(cfg.Window, start, official, haveOfficial))
	fmt.Print(report.Table(rows, haveOfficial))
	return nil
}

// windowLine says which window the numbers cover and whether its boundary came
// from the quota source or was inferred locally.
func windowLine(kind string, start time.Time, official report.Official, haveOfficial bool) string {
	origin := "inferred"
	used := ""
	if haveOfficial {
		origin = "official"
		used = fmt.Sprintf(", %.0f%% used", official.PercentUsed)
	}
	return fmt.Sprintf("window %s: since %s (%s%s)",
		kind, start.Local().Format(time.RFC3339), origin, used)
}
