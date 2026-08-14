// Command ask-quota reports which tasks consumed your Claude Code quota,
// read from local transcripts.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/dendydandees/ask-quota/internal/cli"
	"github.com/dendydandees/ask-quota/internal/quota"
	"github.com/dendydandees/ask-quota/internal/report"
	"github.com/dendydandees/ask-quota/internal/transcript"
	"github.com/dendydandees/ask-quota/internal/window"
)

// version is set by the release build with -ldflags="-X main.version=...".
// A `go install` never runs that, so the module version the toolchain recorded
// stands in — otherwise every installed copy would report "dev".
var version = ""

func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}

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
		fmt.Printf("ask-quota %s\n", versionString())
		return
	}

	if err := run(cfg, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "ask-quota:", err)
		os.Exit(2)
	}
}

// lookupOfficial is a variable so tests can drive the official branch without
// a network or a token. The seam lives here rather than in the quota package,
// where an override would have to name the host the bearer token is sent to.
var lookupOfficial = quota.Lookup

func run(cfg cli.Config, w, errw io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	root := filepath.Join(home, ".claude", "projects")
	now := time.Now()

	// The official boundary is optional: without it the window is inferred or
	// rolling and the QUOTA column is dropped, but nothing else changes. It runs
	// alongside the scan so the lookup costs no wall clock — and so its timeout
	// can be generous enough not to degrade on a slow network.
	type lookup struct {
		official quota.Official
		reason   string
	}
	pending := make(chan lookup, 1)
	go func() {
		o, reason := lookupOfficial(cfg.Window)
		pending <- lookup{o, reason}
	}()

	// Scanned wider than the window: a session block is defined by the idle
	// gap before it, which is only visible from earlier activity.
	scanned, err := transcript.Scan(root, window.Lookback(cfg.Window, now))
	if err != nil {
		return err
	}

	// An absent boundary is the zero Official, which is exactly what the window
	// resolver already treats as "no official boundary".
	got := <-pending
	official, haveOfficial := got.official, got.reason == ""

	// Printed here rather than beside the table, so every exit below carries
	// it: an empty window and --json are degraded runs too, and the empty-window
	// line quotes a start time that may itself be a guess.
	if hint := sourceHint(cfg.Window, got.reason); hint != "" {
		fmt.Fprintln(errw, hint)
	}

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

// sourceHint says why a run fell back to a guess, on the runs where that is
// news. Its caller writes it to stderr — advice, not report, so it neither
// shifts the table's line layout for anything piping stdout nor lands in a
// redirected file. Silent for 30d, which never asks for an official boundary and so is
// not degraded by lacking one, and silent when the boundary was found.
//
// The reason is named rather than summarised: an expired sign-in, a rate limit
// and an unreachable network have different fixes, and one generic line would
// send the user looking in the wrong place.
func sourceHint(kind, reason string) string {
	if reason == "" || kind == window.Month {
		return ""
	}
	return "ask-quota: " + reason
}

// windowLine says which window the numbers cover and where its boundary came
// from. Three origins, not two: the API, a session block guessed from idle
// gaps, or a plain rolling span back from now. Only the guess is
// "inferred" — a rolling window is exact by definition, so labelling 30d
// inferred claimed an uncertainty that does not exist.
func windowLine(kind string, start time.Time, official quota.Official, haveOfficial bool) string {
	origin := "rolling"
	used := ""
	switch {
	case haveOfficial:
		origin = "official"
		used = fmt.Sprintf(", %.0f%% used", official.PercentUsed)
	case kind == window.Session:
		origin = "inferred"
	}
	return fmt.Sprintf("window %s: since %s (%s%s)",
		kind, start.Local().Format(time.RFC3339), origin, used)
}
