package vm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Leovoss/anviq-microvm-engine/internal/proto"
)

const guestDialTimeout = 10 * time.Second

// Exec runs a command inside the microVM and streams ProcessEvents to emit.
// The final event is always of type "exit".
//
// Transport: a host-initiated vsock connection to the anviq-guest agent (see
// internal/guest and cmd/anviq-guest). One connection carries the exec request
// then the guest's NDJSON event stream, which is forwarded to emit unchanged.
func (m *Manager) Exec(ctx context.Context, id string, req ExecRequest, emit func(ProcessEvent) error) error {
	conn, err := m.dial(ctx, id)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Cancel/timeout tears the connection down so DecodeEvents returns promptly.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	if len(req.Argv) == 0 {
		return fmt.Errorf("argv is required")
	}
	err = proto.WriteRequest(conn, proto.Request{
		Op:   proto.OpExec,
		Argv: req.Argv,
		Cwd:  req.Cwd,
		Env:  req.Env,
		PTY:  req.PTY,
	})
	if err != nil {
		return fmt.Errorf("send exec to guest %s: %w", id, err)
	}
	return proto.DecodeEvents(conn, emit)
}

// ListFiles returns directory entries inside the guest.
func (m *Manager) ListFiles(ctx context.Context, id, path string) ([]proto.FileEntry, error) {
	conn, err := m.dial(ctx, id)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := proto.WriteRequest(conn, proto.Request{Op: proto.OpList, Path: path}); err != nil {
		return nil, err
	}
	var entries []proto.FileEntry
	err = json.NewDecoder(conn).Decode(&entries)
	return entries, err
}

// ReadFile returns the bytes of a file inside the guest.
func (m *Manager) ReadFile(ctx context.Context, id, path string) ([]byte, error) {
	conn, err := m.dial(ctx, id)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if err := proto.WriteRequest(conn, proto.Request{Op: proto.OpRead, Path: path}); err != nil {
		return nil, err
	}
	return io.ReadAll(conn)
}

// WriteFile writes bytes to a file inside the guest.
func (m *Manager) WriteFile(ctx context.Context, id, path string, content []byte) error {
	conn, err := m.dial(ctx, id)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := proto.WriteRequest(conn, proto.Request{Op: proto.OpWrite, Path: path, Content: content}); err != nil {
		return err
	}
	// Guest acknowledges with a single JSON line: {"ok":true} or {"error":"..."}.
	line, _ := bufio.NewReader(conn).ReadString('\n')
	var ack struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal([]byte(line), &ack)
	if !ack.OK {
		return fmt.Errorf("guest write failed: %s", ack.Error)
	}
	return nil
}

// dial resolves a running box and opens a vsock connection to its guest agent.
func (m *Manager) dial(ctx context.Context, id string) (io.ReadWriteCloser, error) {
	box := m.Get(id)
	if box == nil {
		return nil, fmt.Errorf("unknown sandbox %s", id)
	}
	if box.State != StateRunning {
		return nil, fmt.Errorf("sandbox %s is %s, not running", id, box.State)
	}
	_ = ctx // reserved for a future context-aware dial; cancellation is handled by AfterFunc in Exec
	conn, err := dialGuest(box.vsockUDS, proto.GuestPort, guestDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("connect guest %s: %w", id, err)
	}
	return conn, nil
}
