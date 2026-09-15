// Package vm owns the Firecracker microVM lifecycle for Plan A: raw firecracker
// processes on a single host, one TAP per VM on a shared bridge, CoW ext4 overlays.
package vm

import (
	"time"

	"github.com/Leovoss/anviq-microvm-engine/internal/proto"
)

// State mirrors the openapi Sandbox.state enum.
type State string

const (
	StateBooting   State = "booting"
	StateRunning   State = "running"
	StatePaused    State = "paused"
	StateDestroyed State = "destroyed"
)

// Sandbox is one microVM. It is the unit the control plane hands back to clients;
// its fields build a rakazo ComputerRef directly (ID -> providerRef, TeamID -> tenant).
type Sandbox struct {
	ID        string    `json:"id"`
	TeamID    string    `json:"team_id"`
	State     State     `json:"state"`
	IP        string    `json:"ip"`
	VCPUs     int64     `json:"vcpus"`
	MemMiB    int64     `json:"mem_mib"`
	Fresh     bool      `json:"fresh"`
	CreatedAt time.Time `json:"created_at"`

	// Host-side bookkeeping, not serialized to clients.
	tapDevice   string
	socketPath  string
	overlayPath string
	vsockUDS    string // per-VM Unix socket Firecracker exposes for host<->guest vsock
	cid         uint32 // guest context id for the vsock device (unique per VM, >= 3)
}

// CreateRequest is the decoded body of POST /v1/sandboxes.
type CreateRequest struct {
	TeamID       string `json:"team_id"`
	VCPUs        int64  `json:"vcpus"`
	MemMiB       int64  `json:"mem_mib"`
	FromSnapshot string `json:"from_snapshot"`
}

// ExecRequest is the decoded body of POST /v1/sandboxes/{id}/exec.
type ExecRequest struct {
	Argv      []string          `json:"argv"`
	Cwd       string            `json:"cwd"`
	Env       map[string]string `json:"env"`
	PTY       bool              `json:"pty"`
	TimeoutMs int64             `json:"timeout_ms"`
}

// ProcessEvent is one line of the NDJSON exec stream. It aliases the canonical
// definition in internal/proto so there is a single source of truth from the
// guest agent all the way out to the HTTP client (and rakazo's ProcessEvent).
type ProcessEvent = proto.ProcessEvent

// SnapshotRef matches rakazo's SnapshotRef.
type SnapshotRef struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// HostConfig is resolved once at startup from flags/host-setup.sh output.
type HostConfig struct {
	KernelPath   string // /opt/anviq/vmlinux
	BaseRootfs   string // /opt/anviq/base.ext4 (read-only)
	StateDir     string // /var/lib/anviq
	Bridge       string // br0
	SubnetCIDR   string // 192.168.100.0/24
	GatewayIP    string // 192.168.100.1
	FirecrackerBin string // /usr/local/bin/firecracker
}
