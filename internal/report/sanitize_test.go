package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dendy-fisiohome/ask-quota/internal/transcript"
)

// Bytes and runes that let transcript text misrepresent itself on a terminal.
var hostile = []string{"\x1b", "\x07", "\x7f", "\u009b", "\u202e", "\u2066"}

const payload = "PAY\x1b[31mRED\x07LOAD \u202egnp.eciovni\u009b tail"

// The table stripped these but --json did not, and jq decodes JSON's escaping
// back into raw bytes on the way to the terminal — the pipeline this project
// documents. Both renderers draw from the same rows, so the sanitising has to
// happen before either of them.
func TestJSONCarriesNoTerminalControlSequences(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session(payload, "/home/u/\x1b[2Jevil", transcript.Usage{Output: 1}),
	})

	data, err := JSON(rows)
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("not a JSON array: %v", err)
	}

	// Compared after decoding, which is what jq hands to the terminal.
	for _, field := range []string{"task", "project", "file"} {
		v, _ := got[0][field].(string)
		for _, bad := range hostile {
			if strings.Contains(v, bad) {
				t.Errorf("%s carries %q after decoding: %q", field, bad, v)
			}
		}
	}
}

func TestTableCarriesNoTerminalControlSequences(t *testing.T) {
	rows := Summarize([]transcript.Session{
		session(payload, "/home/u/\x1b[2Jevil", transcript.Usage{Output: 1}),
	})

	out := Table(Rank(rows, 0), false)
	for _, bad := range hostile {
		if strings.Contains(out, bad) {
			t.Errorf("table carries %q:\n%q", bad, out)
		}
	}
	if !strings.Contains(out, "PAY[31mREDLOAD") {
		t.Errorf("stripping removed legitimate text:\n%s", out)
	}
}

// Newlines must not simply vanish, or two words become one.
func TestSanitizeKeepsWordsApart(t *testing.T) {
	if got, want := sanitize("multi\nline\tprompt"), "multi line prompt"; got != want {
		t.Errorf("sanitize = %q, want %q", got, want)
	}
}
