package vm

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
)

// newOverlay creates a per-VM rootfs cloned from the shared base image.
//
// Firecracker only accepts RAW block devices (no qcow2), so the per-VM disk must be
// a raw ext4 file. `cp --reflink=auto` gives a real copy-on-write clone in
// milliseconds on filesystems that support it (btrfs, xfs, bcachefs) and falls back
// to a full copy elsewhere — correct either way, fast where it counts.
//
// Plan B swaps this for devmapper thin snapshots at high density; the signature
// stays the same, so only this function changes.
func (m *Manager) newOverlay(id string) (string, error) {
	overlay := filepath.Join(m.cfg.StateDir, id+".ext4")
	cmd := exec.Command("cp", "--reflink=auto", "--sparse=always", m.cfg.BaseRootfs, overlay)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("clone rootfs for %s: %w: %s", id, err, out)
	}
	return overlay, nil
}

// shortID returns a short random suffix for VM ids.
func shortID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
