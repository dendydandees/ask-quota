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
	"slices"
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
	Modified time.Time
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

	for i, tf := range files {
		// Acquired before the goroutine exists, so the number of files bounds
		// the work in flight rather than the number of goroutines alive.
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			// A file we cannot read is skipped, not fatal: one unreadable
			// transcript should not cost the user the whole report.
			if s, err := readSession(tf.path, since); err == nil {
				s.Modified = tf.modified
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
	return claimOnce(out), nil
}

// claimOnce drops messages already counted by an earlier session.
//
// Resuming or compacting a conversation writes a fresh transcript that replays
// earlier assistant turns with their original ids and timestamps, so per-file
// deduplication is not enough: the copies land in the same window and would be
// counted again. A message belongs to the session that saw it first, which puts
// the usage on the conversation that originally incurred it and makes a pure
// replay disappear rather than linger as a zero row.
//
// This runs after the scan's goroutines have joined, so it needs no lock.
func claimOnce(sessions []Session) []Session {
	slices.SortFunc(sessions, func(a, b Session) int {
		if d := a.start().Compare(b.start()); d != 0 {
			return d
		}
		// A resume replays the original's turns with their original timestamps,
		// so both sessions often report the same first message. The original
		// file was written first, so its mtime breaks the tie meaningfully —
		// where a UUID filename would decide it by coin flip. Path remains the
		// last resort so the result is always deterministic.
		if d := a.Modified.Compare(b.Modified); d != 0 {
			return d
		}
		return strings.Compare(a.File, b.File)
	})

	claimed := make(map[string]bool)
	out := sessions[:0]
	for _, s := range sessions {
		kept := s.Messages[:0]
		for _, m := range s.Messages {
			if m.ID != "" {
				if claimed[m.ID] {
					continue
				}
				claimed[m.ID] = true
			}
			kept = append(kept, m)
		}
		if len(kept) > 0 {
			s.Messages = kept
			out = append(out, s)
		}
	}
	return out
}

// start is the session's earliest message, which orders sessions by when the
// conversation began rather than by when its file was written.
func (s Session) start() time.Time {
	if len(s.Messages) == 0 {
		return time.Time{}
	}
	first := s.Messages[0].Time
	for _, m := range s.Messages[1:] {
		if m.Time.Before(first) {
			first = m.Time
		}
	}
	return first
}

// findTranscripts lists transcript files that could hold in-window messages.
// Transcripts are append-only, so a file last modified before the window
// cannot contain a message inside it — skipping those is what keeps the
// 30-day mode fast.
// transcriptFile is a candidate file and the modification time the walk already
// had to read, kept so sessions can be ordered without a second stat.
type transcriptFile struct {
	path     string
	modified time.Time
}

func findTranscripts(root string, since time.Time) ([]transcriptFile, error) {
	var files []transcriptFile
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
		files = append(files, transcriptFile{path, info.ModTime()})
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
	// id -> index in s.Messages, so a later, more complete copy can replace an
	// earlier partial one.
	seen := make(map[string]int)

	// A Scanner would be simpler, but it stops dead on a line larger than its
	// buffer and takes the rest of the file with it. Transcripts can carry a
	// pasted image on one line, so oversized lines are skipped individually and
	// reading continues.
	br := bufio.NewReaderSize(f, 64*1024)
	for {
		line, readErr := readCappedLine(br)

		if len(line) > 0 {
			readLine(line, since, &s, seen)
		}
		if readErr != nil {
			break
		}
	}
	return s, nil
}

// maxLine bounds one line. The largest line in a real 660 MB corpus was under
// 1 MB, so this leaves an order of magnitude for a pasted image while keeping a
// runaway line cheap to discard.
//
// The real ceiling is higher than it reads: append overshoots by up to 2x, and
// NumCPU files are read at once. At 8 MiB that is ~128 MiB on an 8-core box;
// at 32 MiB it was over half a gigabyte.
const maxLine = 8 << 20

// readCappedLine returns the next line, or nil for a line past maxLine, having
// consumed it either way. Dropping one oversized line keeps the rest of the
// file readable, which a fixed-buffer Scanner does not.
func readCappedLine(br *bufio.Reader) ([]byte, error) {
	var line []byte
	over := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !over && len(line)+len(chunk) > maxLine {
			// Past the cap: drop what was collected and stop retaining. The
			// loop still runs to consume the line, or the next read would
			// resume in the middle of it.
			line, over = nil, true
		}
		if !over {
			line = append(line, chunk...)
		}
		if err != bufio.ErrBufferFull {
			return line, err
		}
	}
}

// readLine folds one transcript line into s.
func readLine(line []byte, since time.Time, s *Session, seen map[string]int) {
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
	if s.CWD == "" {
		s.CWD = r.CWD
	}
	msg := Message{
		ID:   r.Message.ID,
		Time: r.Timestamp,
		Usage: Usage{
			Input:      r.Message.Usage.Input,
			Output:     r.Message.Usage.Output,
			CacheWrite: r.Message.Usage.CacheWrite,
			CacheRead:  r.Message.Usage.CacheRead,
		},
	}

	// Every assistant message is written ~3x per transcript, and the copies are
	// streaming snapshots rather than repeats: the first is written before the
	// turn has finished. Counting all three inflates ~3x; keeping the first
	// undercounts output by ~7.5%. The most complete copy is the true record.
	if r.Message.ID != "" {
		if i, ok := seen[r.Message.ID]; ok {
			if msg.Usage.Output > s.Messages[i].Usage.Output {
				s.Messages[i] = msg
			}
			return
		}
		seen[r.Message.ID] = len(s.Messages)
	}
	s.Messages = append(s.Messages, msg)
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
