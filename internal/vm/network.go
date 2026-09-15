package vm

import (
	"fmt"
	"os/exec"
)

// setupTAP creates a TAP device for one microVM and attaches it to the host bridge.
// Plan A networking: one bridge (created once by host-setup.sh), one TAP per VM, host NAT.
// Plan B replaces this with a CNI plugin (tc-redirect-tap) and an overlay; the manager
// calls the same two functions, so only this file changes.
func setupTAP(bridge, tap string) error {
	steps := [][]string{
		{"ip", "tuntap", "add", "dev", tap, "mode", "tap"},
		{"ip", "link", "set", tap, "master", bridge},
		{"ip", "link", "set", "dev", tap, "up"},
	}
	for _, args := range steps {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("network setup %v: %w: %s", args, err, out)
		}
	}
	return nil
}

// teardownTAP removes the TAP device. Called from destroy; must not leak on the host.
func teardownTAP(tap string) error {
	if out, err := exec.Command("ip", "link", "del", tap).CombinedOutput(); err != nil {
		return fmt.Errorf("network teardown %s: %w: %s", tap, err, out)
	}
	return nil
}

// tapName derives a deterministic, interface-name-length-safe TAP name from a VM id.
func tapName(vmID string) string {
	const max = 15 // Linux IFNAMSIZ - 1
	name := "tap-" + vmID
	if len(name) > max {
		name = name[:max]
	}
	return name
}
