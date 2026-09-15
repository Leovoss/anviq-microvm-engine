// Command fcctl is the Anviq microVM control plane (Plan A: single host).
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/Leovoss/anviq-microvm-engine/internal/server"
	"github.com/Leovoss/anviq-microvm-engine/internal/vm"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "control-plane listen address")
	stateDir := flag.String("state-dir", "/var/lib/anviq", "where VM registry, overlays, and sockets live")
	assetDir := flag.String("asset-dir", "/opt/anviq", "kernel + base rootfs directory (from host-setup.sh)")
	bridge := flag.String("bridge", "br0", "host bridge for VM TAP devices")
	subnet := flag.String("subnet", "192.168.100.0/24", "VM subnet")
	gateway := flag.String("gateway", "192.168.100.1", "bridge gateway IP")
	fcBin := flag.String("firecracker", "/usr/local/bin/firecracker", "path to the firecracker binary")
	flag.Parse()

	// The one secret: a host-to-host bearer token shared with the single client.
	token := os.Getenv("ANVIQ_CONTROL_TOKEN")
	if token == "" {
		log.Fatal("ANVIQ_CONTROL_TOKEN must be set (host-to-host bearer token)")
	}

	mgr, err := vm.NewManager(vm.HostConfig{
		KernelPath:     *assetDir + "/vmlinux",
		BaseRootfs:     *assetDir + "/base.ext4",
		StateDir:       *stateDir,
		Bridge:         *bridge,
		SubnetCIDR:     *subnet,
		GatewayIP:      *gateway,
		FirecrackerBin: *fcBin,
	})
	if err != nil {
		log.Fatalf("init manager: %v", err)
	}

	srv := server.New(mgr, token)
	log.Printf("anviq control plane listening on %s (state %s)", *listen, *stateDir)
	if err := http.ListenAndServe(*listen, srv.Handler()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
