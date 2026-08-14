// Package cli parses and validates the command line.
package cli

import (
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/dendydandees/ask-quota/internal/window"
)

// How rows are grouped in the report.
const (
	GroupSession = "session"
	GroupProject = "project"
)

var groupings = []string{GroupSession, GroupProject}

// Intent is what the command line asks for.
type Intent int

const (
	IntentReport Intent = iota
	IntentHelp
	IntentVersion
)

// Config is a validated command line.
type Config struct {
	Intent  Intent
	Window  string
	Top     int // 0 means every row
	GroupBy string
	JSON    bool
}

// asksFor reports whether args contain a bare flag, wherever it appears.
// Help and version are answered before anything else is validated: someone
// asking how the tool works should not first be told they typed it wrong.
func asksFor(args []string, names ...string) bool {
	return slices.ContainsFunc(args, func(a string) bool { return slices.Contains(names, a) })
}

// Parse reads args, applies defaults, and validates. The returned error is
// meant to be shown to the user as-is.
func Parse(args []string) (Config, error) {
	if asksFor(args, "--help", "-h", "help") {
		return Config{Intent: IntentHelp}, nil
	}
	if asksFor(args, "--version", "-v") {
		return Config{Intent: IntentVersion}, nil
	}

	// flag stops parsing at the first non-flag argument, so a leading window
	// would swallow every flag after it. Take it off the front first.
	var positional string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		positional, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("ask-quota", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors are returned, not printed twice
	fs.Usage = func() {}

	win := fs.String("window", window.Session, "")
	top := fs.String("top", "5", "")
	groupBy := fs.String("group-by", GroupSession, "")
	asJSON := fs.Bool("json", false, "")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	cfg := Config{Window: *win, GroupBy: *groupBy, JSON: *asJSON}

	// The window doubles as a positional, so `ask-quota week` reads naturally.
	rest := fs.Args()
	if positional == "" && len(rest) > 0 {
		positional, rest = rest[0], rest[1:]
	}
	if len(rest) > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", rest[0])
	}
	if positional != "" {
		cfg.Window = positional
	}

	if err := oneOf("window", cfg.Window, window.Kinds); err != nil {
		return Config{}, err
	}
	if err := oneOf("--group-by", cfg.GroupBy, groupings); err != nil {
		return Config{}, err
	}

	n, err := parseTop(*top)
	if err != nil {
		return Config{}, err
	}
	cfg.Top = n

	return cfg, nil
}

// oneOf rejects a value outside allowed, naming the alternatives so the user
// does not have to go looking for --help.
func oneOf(name, value string, allowed []string) error {
	if slices.Contains(allowed, value) {
		return nil
	}
	return fmt.Errorf("unknown %s %q: want one of %v", name, value, allowed)
}

func parseTop(s string) (int, error) {
	if s == "all" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid --top %q: want a positive number or \"all\"", s)
	}
	return n, nil
}
