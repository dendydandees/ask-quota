package quota

import (
	"bytes"
	"testing"
)

// The source's output is read into memory, so a runaway source must hit a
// ceiling rather than accumulate until the timeout fires. Every write is still
// reported as accepted, or the child would block on a full pipe instead of
// running to its kill.
func TestCappedBufferStopsAtTheCeiling(t *testing.T) {
	var b cappedBuffer
	chunk := bytes.Repeat([]byte("x"), 64<<10)

	for range 64 { // 4 MB written against a 1 MB ceiling
		n, err := b.Write(chunk)
		if n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v; want the whole chunk accepted", n, err)
		}
	}
	if len(b.data) != maxOutput {
		t.Errorf("retained %d bytes, want the %d-byte ceiling", len(b.data), maxOutput)
	}
}

// The cap must not corrupt a payload that fits, which is every real one.
func TestCappedBufferKeepsAPayloadThatFits(t *testing.T) {
	var b cappedBuffer
	if _, err := b.Write([]byte(sample)); err != nil {
		t.Fatal(err)
	}
	if got, ok := parse(b.data, "5h"); !ok || got.PercentUsed != 51 {
		t.Errorf("parse after buffering = %+v, %v; want the payload intact", got, ok)
	}
}

// A write landing exactly on the ceiling must fill it, not overshoot or stop early.
func TestCappedBufferFillsExactlyToTheCeiling(t *testing.T) {
	var b cappedBuffer
	if _, err := b.Write(bytes.Repeat([]byte("x"), maxOutput+1)); err != nil {
		t.Fatal(err)
	}
	if len(b.data) != maxOutput {
		t.Errorf("retained %d bytes, want exactly %d", len(b.data), maxOutput)
	}
}
