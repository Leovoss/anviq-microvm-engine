package vm

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
)

// newOverlay creates a per-VM copy-on-write rootfs backed by the shared read-only
// base image, so a fresh VM's disk is created in milliseconds instead of copying GBs.
//
// Plan A uses a qcow2-style overlay via qemu-img (works on plain ext4 hosts). Plan B
// swaps this for devmapper thin snapshots at high density — same signature, so only
// this function changes.
func (m *Manager) newOverlay(id string) (string, error) {
	overlay := filepath.Join(m.cfg.StateDir, id+".ext4")
	cmd := exec.Command("qemu-img", "create",
		"-f", "qcow2",
		"-F", "raw",
		"-b", m.cfg.BaseRootfs,
		overlay,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("create overlay for %s: %w: %s", id, err, out)
	}
	return overlay, nil
}

// shortID returns a short random suffix for VM ids.
func shortID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
