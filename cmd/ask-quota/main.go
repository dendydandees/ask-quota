// Command ask-quota reports which tasks consumed your Claude Code quota,
// read from local transcripts.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dendy-fisiohome/ask-quota/internal/report"
	"github.com/dendy-fisiohome/ask-quota/internal/transcript"
	"github.com/dendy-fisiohome/ask-quota/internal/window"
)

// version is overridden at build time with -ldflags="-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("ask-quota %s\n", version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ask-quota:", err)
		os.Exit(2)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root := filepath.Join(home, ".claude", "projects")
	now := time.Now()
	kind := window.Session

	// Scanned wider than the window: a session block is defined by the idle
	// gap before it, which is only visible from earlier activity.
	scanned, err := transcript.Scan(root, window.Lookback(kind, now))
	if err != nil {
		return err
	}

	start, err := window.Resolve(kind, now, time.Time{}, transcript.Times(scanned))
	if err != nil {
		return err
	}

	sessions := transcript.Since(scanned, start)
	if len(sessions) == 0 {
		fmt.Printf("No Claude Code usage found since %s.\n", start.Format(time.RFC3339))
		return nil
	}

	rows := report.Rank(report.Summarize(sessions), 5)
	fmt.Printf("window %s: since %s (inferred)\n", kind, start.Format(time.RFC3339))
	fmt.Print(report.Table(rows))
	return nil
}
