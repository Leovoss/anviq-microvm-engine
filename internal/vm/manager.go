package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/firecracker-microvm/firecracker-go-sdk"
	models "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
)

// Manager owns all microVMs on this host. Plan A: in-process map + a JSON registry
// on disk so an fcctl restart reconnects existing VMs instead of orphaning them.
type Manager struct {
	cfg      HostConfig
	mu       sync.Mutex
	machines map[string]*firecracker.Machine
	boxes    map[string]*Sandbox
	ipAlloc  *ipAllocator
}

func NewManager(cfg HostConfig) (*Manager, error) {
	if err := os.MkdirAll(cfg.StateDir, 0o750); err != nil {
		return nil, err
	}
	alloc, err := newIPAllocator(cfg.SubnetCIDR, cfg.GatewayIP)
	if err != nil {
		return nil, err
	}
	m := &Manager{
		cfg:      cfg,
		machines: map[string]*firecracker.Machine{},
		boxes:    map[string]*Sandbox{},
		ipAlloc:  alloc,
	}
	m.loadRegistry() // best-effort reconnect of prior VMs
	return m, nil
}

// Create provisions a microVM. Per the rakazo contract it returns as soon as the
// box is registered and booting; fallible guest setup happens in Prepare.
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*Sandbox, error) {
	if req.VCPUs == 0 {
		req.VCPUs = 2
	}
	if req.MemMiB == 0 {
		req.MemMiB = 2048
	}
	id := fmt.Sprintf("vm-%s-%s", req.TeamID, shortID())
	ip, err := m.ipAlloc.next()
	if err != nil {
		return nil, err
	}
	tap := tapName(id)
	if err := setupTAP(m.cfg.Bridge, tap); err != nil {
		return nil, err
	}
	overlay, err := m.newOverlay(id)
	if err != nil {
		_ = teardownTAP(tap)
		return nil, err
	}

	box := &Sandbox{
		ID:          id,
		TeamID:      req.TeamID,
		State:       StateBooting,
		IP:          ip,
		VCPUs:       req.VCPUs,
		MemMiB:      req.MemMiB,
		Fresh:       true,
		CreatedAt:   time.Now().UTC(),
		tapDevice:   tap,
		socketPath:  filepath.Join(m.cfg.StateDir, id+".sock"),
		overlayPath: overlay,
	}

	machine, err := m.boot(ctx, box)
	if err != nil {
		_ = teardownTAP(tap)
		return nil, err
	}

	m.mu.Lock()
	m.machines[id] = machine
	m.boxes[id] = box
	m.mu.Unlock()
	m.saveRegistry()

	box.State = StateRunning
	return box, nil
}

// boot builds the Firecracker config for one microVM and starts it. This is the
// heart of Plan A — the boot path everything else layers on top of.
func (m *Manager) boot(ctx context.Context, box *Sandbox) (*firecracker.Machine, error) {
	cfg := firecracker.Config{
		SocketPath:      box.socketPath,
		KernelImagePath: m.cfg.KernelPath,
		KernelArgs:      "console=ttyS0 reboot=k panic=1 pci=off",
		Drives: []models.Drive{{
			DriveID:      firecracker.String("rootfs"),
			PathOnHost:   firecracker.String(box.overlayPath),
			IsRootDevice: firecracker.Bool(true),
			IsReadOnly:   firecracker.Bool(false),
		}},
		NetworkInterfaces: []firecracker.NetworkInterface{{
			StaticConfiguration: &firecracker.StaticNetworkConfiguration{
				HostDevName: box.tapDevice,
			},
		}},
		MachineCfg: models.MachineConfiguration{
			VcpuCount:  firecracker.Int64(box.VCPUs),
			MemSizeMib: firecracker.Int64(box.MemMiB),
		},
	}

	cmd := firecracker.VMCommandBuilder{}.
		WithBin(m.cfg.FirecrackerBin).
		WithSocketPath(box.socketPath).
		Build(ctx)

	machine, err := firecracker.NewMachine(ctx, cfg, firecracker.WithProcessRunner(cmd))
	if err != nil {
		return nil, fmt.Errorf("new machine %s: %w", box.ID, err)
	}
	if err := machine.Start(ctx); err != nil {
		return nil, fmt.Errorf("start %s: %w", box.ID, err)
	}
	return machine, nil
}

// Get returns a running box by id, or nil if unknown (client then creates fresh).
func (m *Manager) Get(id string) *Sandbox {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.boxes[id]
}

// Stop pauses the microVM (reversible). Resources are released; state persists.
func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.Lock()
	machine, box := m.machines[id], m.boxes[id]
	m.mu.Unlock()
	if machine == nil {
		return fmt.Errorf("unknown sandbox %s", id)
	}
	if err := machine.PauseVM(ctx); err != nil {
		return err
	}
	box.State = StatePaused
	m.saveRegistry()
	return nil
}

// Destroy tears the microVM down and reclaims every host resource. Irreversible.
func (m *Manager) Destroy(ctx context.Context, id string) error {
	m.mu.Lock()
	machine, box := m.machines[id], m.boxes[id]
	delete(m.machines, id)
	delete(m.boxes, id)
	m.mu.Unlock()
	if box == nil {
		return nil // already gone; destroy is idempotent
	}
	if machine != nil {
		_ = machine.StopVMM()
	}
	_ = teardownTAP(box.tapDevice)
	_ = os.Remove(box.overlayPath)
	_ = os.Remove(box.socketPath)
	m.ipAlloc.release(box.IP)
	box.State = StateDestroyed
	m.saveRegistry()
	return nil
}

func (m *Manager) registryPath() string { return filepath.Join(m.cfg.StateDir, "registry.json") }

func (m *Manager) saveRegistry() {
	m.mu.Lock()
	snapshot := make([]*Sandbox, 0, len(m.boxes))
	for _, b := range m.boxes {
		snapshot = append(snapshot, b)
	}
	m.mu.Unlock()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(m.registryPath(), data, 0o640)
}

// loadRegistry rehydrates box metadata after an fcctl restart. Reattaching to the
// live firecracker process by socket is a Phase 1 TODO; today we surface prior boxes
// as paused so the client can decide to resume or destroy.
func (m *Manager) loadRegistry() {
	data, err := os.ReadFile(m.registryPath())
	if err != nil {
		return
	}
	var prior []*Sandbox
	if json.Unmarshal(data, &prior) != nil {
		return
	}
	for _, b := range prior {
		if b.State != StateDestroyed {
			b.State = StatePaused
			m.boxes[b.ID] = b
			m.ipAlloc.reserve(b.IP)
		}
	}
}
