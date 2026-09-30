# anviq-microvm-engine

A control plane for [Firecracker](https://firecracker-microvm.github.io/) microVM sandboxes, written in Go. Each sandbox is a KVM guest with its own kernel, created from a copy-on-write rootfs and driven over a small HTTP API. A guest agent inside the VM handles exec and file operations over vsock, so the host never needs SSH or a network path into the guest.

The API follows [rakazo](https://github.com/elie222/rakazo)'s `SandboxProvider` contract, so it can back rakazo as a `firecracker` provider. See [`docs/rakazo-seam.md`](docs/rakazo-seam.md).

## Status

Work in progress. The control plane, the guest agent and the host/guest protocol are implemented and unit-tested (`go test ./...` runs without KVM). Booting on real hardware is next:

- [ ] `fcctl` boots a microVM on a KVM host through `POST /v1/sandboxes`
- [ ] `make smoke` runs a command in the VM and tears it down cleanly
- [ ] `stop` pauses and snapshots, a later provision resumes it
- [ ] `destroy` leaves no orphaned TAP device, overlay, socket or process
- [ ] The rakazo adapter passes rakazo's sandbox conformance tests

Multi-host scheduling, `jailer` and cgroups limits are designed (see [`docs/architecture.md`](docs/architecture.md)) but not built.

## Layout

```
api/openapi.yaml       HTTP contract
cmd/fcctl/             control plane daemon
cmd/anviq-guest/       guest agent (runs inside the VM)
internal/vm/           Firecracker lifecycle, TAP networking, CoW rootfs, vsock
internal/server/       HTTP handlers
internal/proto/        host/guest exec and file protocol
adapter/               rakazo SandboxProvider adapter (TypeScript)
scripts/               host setup, rootfs build, smoke test
docs/                  architecture and the rakazo mapping
```

## Running it

Needs a Linux host with KVM (`/dev/kvm`) and root, or `CAP_NET_ADMIN` plus KVM access. It won't run inside an unprivileged container.

```bash
# one-time host prep: bridge, NAT, kernel and base rootfs into /opt/anviq
sudo ./scripts/host-setup.sh

export ANVIQ_CONTROL_TOKEN=$(openssl rand -hex 32)
make run    # builds and starts fcctl on 127.0.0.1:8080

AUTH="Authorization: Bearer $ANVIQ_CONTROL_TOKEN"
curl -sX POST localhost:8080/v1/sandboxes -H "$AUTH" -d '{"team_id":"demo","vcpus":2,"mem_mib":2048}'
curl -sX POST localhost:8080/v1/sandboxes/<id>/exec -H "$AUTH" -d '{"argv":["uname","-a"]}'
```

`fcctl --help` lists the flags (state dir, asset dir, bridge, subnet, firecracker path).

## Tests

```bash
go test ./...
```

## License

Source is published for reference only. All rights reserved, see [LICENSE](LICENSE).
