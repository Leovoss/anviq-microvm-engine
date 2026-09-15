package proto

import (
	"bufio"
	"bytes"
	"testing"
)

// Round-trip a request through the wire encoding. Runs offline (pure stdlib).
func TestRequestRoundTrip(t *testing.T) {
	want := Request{
		Op:   OpExec,
		Argv: []string{"uname", "-a"},
		Cwd:  "/root",
		Env:  map[string]string{"FOO": "bar"},
	}
	var buf bytes.Buffer
	if err := WriteRequest(&buf, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadRequest(bufio.NewReader(&buf))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Op != want.Op || len(got.Argv) != 2 || got.Argv[1] != "-a" || got.Cwd != "/root" || got.Env["FOO"] != "bar" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

// The event stream the guest emits must decode back to the same events the host
// forwards to the HTTP client — this is the whole exec contract in miniature.
func TestEventStreamRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	s := NewEventStream(&buf)
	emit := []ProcessEvent{
		StdoutEvent("Linux fc 6.1.0\n"),
		StderrEvent("warn: nothing\n"),
		ExitEvent(0),
	}
	for _, ev := range emit {
		if err := s.Emit(ev); err != nil {
			t.Fatalf("emit: %v", err)
		}
	}

	var got []ProcessEvent
	if err := DecodeEvents(&buf, func(ev ProcessEvent) error { got = append(got, ev); return nil }); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 events, got %d", len(got))
	}
	if got[0].Type != "stdout" || got[0].Data != "Linux fc 6.1.0\n" {
		t.Fatalf("stdout event wrong: %+v", got[0])
	}
	last := got[2]
	if last.Type != "exit" || last.Code == nil || *last.Code != 0 {
		t.Fatalf("exit event wrong: %+v", last)
	}
}

// Content bytes survive the JSON base64 hop (used by write file ops).
func TestWriteContentBytes(t *testing.T) {
	payload := []byte{0x00, 0x01, 0xff, 'h', 'i'}
	var buf bytes.Buffer
	if err := WriteRequest(&buf, Request{Op: OpWrite, Path: "/x", Content: payload}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadRequest(bufio.NewReader(&buf))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got.Content, payload) {
		t.Fatalf("content mismatch: %v", got.Content)
	}
}
