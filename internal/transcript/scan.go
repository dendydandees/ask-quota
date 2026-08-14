// Package transcript reads Claude Code transcript files and reports the token
// usage recorded in them.
package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Usage is the token count of a single assistant message.
type Usage struct {
	Input      int64
	Output     int64
	CacheWrite int64
	CacheRead  int64
}

// Message is one assistant turn that consumed quota.
type Message struct {
	ID    string
	Time  time.Time
	Usage Usage
}

// Session is one transcript file, with the messages that fall inside the
// requested window.
type Session struct {
	File     string
	CWD      string
	Label    string
	Messages []Message
}

// Scan walks root for transcripts and returns one Session per file that
// recorded usage at or after since. Sessions with no usage in the window are
// omitted. A missing root yields no sessions and no error.
func Scan(root string, since time.Time) ([]Session, error) {
	files, err := findTranscripts(root, since)
	if err != nil {
		return nil, err
	}

	sessions := make([]Session, len(files))
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())

	for i, path := range files {
		// Acquired before the goroutine exists, so the number of files bounds
		// the work in flight rather than the number of goroutines alive.
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			// A file we cannot read is skipped, not fatal: one unreadable
			// transcript should not cost the user the whole report.
			if s, err := readSession(path, since); err == nil {
				sessions[i] = s
			}
		}()
	}
	wg.Wait()

	out := sessions[:0]
	for _, s := range sessions {
		if len(s.Messages) > 0 {
			out = append(out, s)
		}
	}
	return out, nil
}

// findTranscripts lists transcript files that could hold in-window messages.
// Transcripts are append-only, so a file last modified before the window
// cannot contain a message inside it — skipping those is what keeps the
// 30-day mode fast.
func findTranscripts(root string, since time.Time) ([]string, error) {
	var files []string
	// A directory that cannot be walked is skipped rather than fatal, which
	// also covers a missing root: the report is simply empty.
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(since) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}

// record is the subset of a transcript line this tool reads.
type record struct {
	Type      string    `json:"type"`
	CWD       string    `json:"cwd"`
	Timestamp time.Time `json:"timestamp"`
	IsMeta    bool      `json:"isMeta"`
	Message   struct {
		ID      string          `json:"id"`
		Content json.RawMessage `json:"content"`
		Usage   *struct {
			Input      int64 `json:"input_tokens"`
			Output     int64 `json:"output_tokens"`
			CacheWrite int64 `json:"cache_creation_input_tokens"`
			CacheRead  int64 `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func readSession(path string, since time.Time) (Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()

	s := Session{File: path}
	seen := make(map[string]bool)

	// A Scanner would be simpler, but it stops dead on a line larger than its
	// buffer and takes the rest of the file with it. Transcripts can carry a
	// pasted image on one line, so lines are read without a fixed ceiling.
	br := bufio.NewReaderSize(f, 64*1024)
	for {
		line, readErr := br.ReadBytes('\n')

		if len(line) > 0 {
			readLine(line, since, &s, seen)
		}
		if readErr != nil {
			break
		}
	}
	return s, nil
}

// readLine folds one transcript line into s.
func readLine(line []byte, since time.Time, s *Session, seen map[string]bool) {
	// Most lines are user turns and tool results. Rejecting them on a
	// substring before unmarshalling avoids the dominant cost, and matching on
	// []byte directly avoids copying the line to do it.
	needLabel := s.Label == ""
	if !needLabel && !bytes.Contains(line, usageKey) {
		return
	}

	var r record
	// A malformed line is skipped: the last line of an active transcript is
	// often mid-write.
	if json.Unmarshal(line, &r) != nil {
		return
	}

	if needLabel && r.Type == "user" && !r.IsMeta {
		if text := promptText(r.Message.Content); text != "" {
			s.Label = text
		}
	}

	if r.Message.Usage == nil || r.Type != "assistant" || r.Timestamp.Before(since) {
		return
	}
	// Every assistant message is written ~3x per transcript; counting each copy
	// would inflate every number in the tool by ~3x.
	if r.Message.ID != "" {
		if seen[r.Message.ID] {
			return
		}
		seen[r.Message.ID] = true
	}
	if s.CWD == "" {
		s.CWD = r.CWD
	}
	s.Messages = append(s.Messages, Message{
		ID:   r.Message.ID,
		Time: r.Timestamp,
		Usage: Usage{
			Input:      r.Message.Usage.Input,
			Output:     r.Message.Usage.Output,
			CacheWrite: r.Message.Usage.CacheWrite,
			CacheRead:  r.Message.Usage.CacheRead,
		},
	})
}

// usageKey is the marker that tells a quota-consuming line from the rest.
var usageKey = []byte(`"usage"`)

// skipPrefixes mark user lines that are machinery rather than something the
// user typed. Every wrapper the harness injects — slash commands, hooks, task
// notifications, system reminders — arrives as a tag, so one "<" covers them
// all and keeps covering tags added later.
var skipPrefixes = []string{
	"<",
	"[Request interrupted",
	"Caveat:",
	"This session is being continued",
}

// promptText returns the user's own text for a line, or "" if the line is
// machinery, a tool result, or empty.
func promptText(content json.RawMessage) string {
	if len(content) == 0 {
		return ""
	}

	var text string
	if content[0] == '"' {
		if json.Unmarshal(content, &text) != nil {
			return ""
		}
	} else {
		var blocks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(content, &blocks) != nil {
			return ""
		}
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		text = strings.Join(parts, " ")
	}

	text = strings.TrimSpace(text)
	for _, p := range skipPrefixes {
		if strings.HasPrefix(text, p) {
			return ""
		}
	}
	return text
}

// Plus returns the sum of two usage records. Totalling usage is done wherever
// records are folded together, so the arithmetic lives with the type.
func (u Usage) Plus(o Usage) Usage {
	return Usage{
		Input:      u.Input + o.Input,
		Output:     u.Output + o.Output,
		CacheWrite: u.CacheWrite + o.CacheWrite,
		CacheRead:  u.CacheRead + o.CacheRead,
	}
}
