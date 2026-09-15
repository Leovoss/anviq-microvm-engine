// Package proto is the wire contract between the control plane (host) and the
// anviq-guest agent running inside each microVM.
//
// It is deliberately pure stdlib: no vsock, no firecracker dependency. That means
// this package — and its tests — build and run anywhere, including CI without KVM.
// One connection carries one Request (a single JSON line, host -> guest) followed
// by a stream of newline-delimited responses (guest -> host).
package proto

import (
	"bufio"
	"encoding/json"
	"io"
)

// Guest operations.
const (
	OpExec  = "exec"
	OpList  = "list"
	OpRead  = "read"
	OpWrite = "write"
)

// GuestPort is the AF_VSOCK port the guest agent listens on.
const GuestPort = 1024

// Request is one host->guest message (a single JSON line).
type Request struct {
	Op      string            `json:"op"`
	Argv    []string          `json:"argv,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	PTY     bool              `json:"pty,omitempty"`
	Path    string            `json:"path,omitempty"`
	Content []byte            `json:"content,omitempty"` // write payload (JSON base64-encodes []byte)
}

// ProcessEvent is one line of an exec response stream. This is the CANONICAL
// definition; the vm and server layers alias it so there is a single source of
// truth all the way from the guest to the HTTP client.
type ProcessEvent struct {
	Type string `json:"type"`           // "stdout" | "stderr" | "exit"
	Data string `json:"data,omitempty"` // stdout/stderr text
	Code *int   `json:"code,omitempty"` // exit code (exit events only)
}

// FileEntry is one item of a list response.
type FileEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // "file" | "dir"
	Size int64  `json:"size"`
}

// ExitEvent builds a terminal exit event with the given code.
func ExitEvent(code int) ProcessEvent { return ProcessEvent{Type: "exit", Code: &code} }

// StdoutEvent and StderrEvent build output events.
func StdoutEvent(s string) ProcessEvent { return ProcessEvent{Type: "stdout", Data: s} }
func StderrEvent(s string) ProcessEvent { return ProcessEvent{Type: "stderr", Data: s} }

// WriteRequest sends a single request line.
func WriteRequest(w io.Writer, r Request) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// ReadRequest reads a single request line from a buffered reader.
func ReadRequest(r *bufio.Reader) (Request, error) {
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return Request{}, err
	}
	var req Request
	if e := json.Unmarshal(trimNL(line), &req); e != nil {
		return Request{}, e
	}
	return req, nil
}

// EventStream encodes ProcessEvents as newline-delimited JSON. Emit is safe to
// call from a single goroutine; callers streaming from multiple sources must
// serialize their own calls.
type EventStream struct{ enc *json.Encoder }

func NewEventStream(w io.Writer) *EventStream { return &EventStream{enc: json.NewEncoder(w)} }

func (s *EventStream) Emit(ev ProcessEvent) error { return s.enc.Encode(ev) }

// DecodeEvents reads newline-delimited ProcessEvents from r, calling emit for
// each. It returns when the stream ends or emit returns an error.
func DecodeEvents(r io.Reader, emit func(ProcessEvent) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // tolerate large single lines
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev ProcessEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return err
		}
		if err := emit(ev); err != nil {
			return err
		}
	}
	return sc.Err()
}

func trimNL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
