# Architecture

Two deployment shapes share one control-plane API. Plan A is what we build now; Plan B is the
scale-out path. The API contract (`api/openapi.yaml`) does not change between them — only what
runs behind it does. That is the whole point of putting the seam in the HTTP layer.

## Plan A — 1 to 10 teams (current)

Goal: minimum operational overhead. One bare-metal host, no orchestrator, native Linux
primitives only.

```
                 ┌─────────────────────────────────────────┐
   rakazo /      │  fcctl  (Go control plane, HTTP :8080)   │
   any client ──▶│  - sandbox registry (state-dir on disk)  │
   (bearer tok)  │  - firecracker-go-sdk per microVM        │
                 └───────────────┬──────────────────────────┘
                                 │ spawns
             ┌───────────────────┼───────────────────┐
             ▼                   ▼                   ▼
      firecracker          firecracker          firecracker
      (team A microVM)     (team B microVM)     (team C microVM)
      2 vCPU / 2 GB        2 vCPU / 2 GB        2 vCPU / 2 GB
      tap-A ─┐             tap-B ─┐             tap-C ─┐
             └──────────── br0 (192.168.100.1/24) ────┘ ──▶ NAT ──▶ host uplink
      rootfs-A.ext4        rootfs-B.ext4        rootfs-C.ext4
      (CoW overlay on a shared read-only base image)
```

- **Host:** 1 bare-metal server, KVM enabled. 16–32 vCPU / 64 GB RAM comfortably runs ~10 teams
  at 2 vCPU / 2 GB each with headroom.
- **Control plane:** `fcctl`, a single Go binary using `firecracker-go-sdk`. State (which VM
  belongs to which team, its TAP, its overlay path) lives in a JSON registry under
  `--state-dir`, so `fcctl` restarts reconnect existing VMs instead of orphaning them.
- **Networking:** one host bridge `br0`; one TAP device per microVM attached to it; host does
  NAT (masquerade) to the uplink. Static per-VM IPs handed out from the `br0` subnet.
- **Storage:** one read-only base rootfs (`base.ext4`) plus a per-VM copy-on-write overlay, so a
  new VM's disk is created instantly without copying gigabytes.
- **Isolation:** the microVM boundary itself (separate kernel, KVM). No `jailer`, no cgroups
  quotas yet — acceptable at <10 tenants on a trusted single host.

### Why this first
Booting one microVM and running a command inside it exercises the entire hard path: KVM, kernel,
rootfs, TAP networking, and the guest exec protocol. Everything in Plan B is an orchestration
layer on top of a boot path that already works. Prove the metal, then scale.

## Plan B — 100 to 2,000+ teams (designed, not built)

Same API, different substrate. The client cannot tell which one it is talking to.

```
        ┌──────────── Fleet control plane (fcctl in cluster mode) ───────────┐
        │  scheduler: place VM on least-loaded host; track capacity          │
        └───────────────┬───────────────────────────────┬───────────────────┘
                        ▼                               ▼
                  Host Node 01                    Host Node 02
                  ├─ jailer per VM (chroot, uid/gid drop, netns)
                  ├─ cgroups v2: cpu.max, memory.max, io.max per VM
                  ├─ CNI (tc-redirect-tap) + VXLAN/WireGuard overlay
                  └─ devmapper thin snapshots for instant rootfs
```

Deltas from Plan A, each additive:

| Concern | Plan A | Plan B |
|---|---|---|
| Execution | raw `firecracker` | `jailer` wrapping `firecracker` (chroot, uid/gid, netns) |
| Isolation | microVM boundary | + cgroups v2 CPU/mem/IO caps (no noisy neighbours) |
| Networking | static bridge + TAP | CNI `tc-redirect-tap` + VXLAN/WireGuard cross-host overlay |
| Rootfs | CoW ext4 overlay | devmapper thin provisioning / ephemeral snapshots |
| Placement | single host | scheduler across a bare-metal fleet |
| Deploy | systemd unit | Helm chart / Nomad job (Phase 3 needs Helm for air-gap) |

## Guest agent

Inside every microVM runs a tiny agent (`internal/guest`) that the control plane talks to over
vsock. It executes commands, streams stdout/stderr, and serves file ops. The rootfs base image
ships with it as an init service, so a freshly booted VM is immediately ready for `exec`. This
is the same pattern E2B/Daytona use; it keeps the host-side control plane from needing SSH into
guests.

## What stays constant across A and B
- The HTTP contract in `api/openapi.yaml`.
- The `SandboxProvider` mapping in `rakazo-seam.md`.
- The guest agent protocol.
- The rule that no request leaves the host in the data path.
