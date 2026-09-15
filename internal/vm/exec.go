package vm

import (
	"context"
	"fmt"
)

// Exec runs a command inside the microVM and streams ProcessEvents to emit.
// The final event is always of type "exit".
//
// Transport (Plan A): the guest ships an init-time agent (see internal/guest) that
// listens on a vsock port. The control plane dials CID=box + a fixed port, sends the
// ExecRequest as JSON, and reads back NDJSON ProcessEvents. This function is the seam
// where that vsock client plugs in; it is intentionally isolated so the guest protocol
// can be developed and tested against a real booted VM without touching the HTTP layer.
//
// Until the vsock guest client lands, this returns a clear not-implemented exit so the
// smoke test fails loudly rather than silently pretending to run commands.
func (m *Manager) Exec(ctx context.Context, id string, req ExecRequest, emit func(ProcessEvent) error) error {
	box := m.Get(id)
	if box == nil {
		return fmt.Errorf("unknown sandbox %s", id)
	}
	if box.State != StateRunning {
		return fmt.Errorf("sandbox %s is %s, not running", id, box.State)
	}
	if len(req.Argv) == 0 {
		return fmt.Errorf("argv is required")
	}

	// TODO(phase1): dial vsock guest agent on box.IP/CID and stream real output.
	// See docs/roadmap.md Phase 1 checklist item "exec runs uname -a inside the VM".
	code := 127
	_ = emit(ProcessEvent{Type: "stderr", Data: "guest exec transport not yet wired (Phase 1 TODO)\n"})
	return emit(ProcessEvent{Type: "exit", Code: &code})
}
