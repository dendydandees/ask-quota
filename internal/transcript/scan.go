// Package transcript reads Claude Code transcript files and reports the token
// usage recorded in them.
package transcript

import (
	"bufio"
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
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
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
	if os.IsNotExist(err) {
		return nil, nil
	}
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

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		line := sc.Bytes()

		// Most lines are user turns and tool results. Rejecting them on a
		// substring before unmarshalling avoids the dominant cost.
		wantUsage := strings.Contains(string(line), `"usage"`)
		if !wantUsage && s.Label != "" {
			continue
		}

		var r record
		// A malformed line is skipped: the last line of an active transcript
		// is often mid-write.
		if json.Unmarshal(line, &r) != nil {
			continue
		}

		if s.Label == "" && r.Type == "user" && !r.IsMeta {
			if text := promptText(r.Message.Content); text != "" {
				s.Label = text
			}
		}

		if r.Message.Usage == nil || r.Type != "assistant" {
			continue
		}
		if r.Timestamp.Before(since) {
			continue
		}
		// Every assistant message is written ~3x per transcript; counting each
		// copy would inflate every number in the tool by ~3x.
		if r.Message.ID != "" {
			if seen[r.Message.ID] {
				continue
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
	return s, nil
}

// maxLine bounds a single transcript line. Lines carry whole assistant turns
// and can be large; beyond this the line is dropped rather than buffered.
const maxLine = 8 * 1024 * 1024

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
