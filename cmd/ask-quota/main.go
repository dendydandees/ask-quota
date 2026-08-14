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

	// Scaffolding: replaced by real window resolution in Task 5.
	since := time.Now().Add(-5 * time.Hour)

	sessions, err := transcript.Scan(root, since)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Printf("No Claude Code usage found since %s.\n", since.Format(time.RFC3339))
		return nil
	}

	rows := report.Rank(report.Summarize(sessions), 5)
	fmt.Print(report.Table(rows))
	return nil
}
