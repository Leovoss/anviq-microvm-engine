# Guest agent

A tiny init-time process baked into the base rootfs. The control plane talks to it over
**vsock** (never SSH, never a network port), so exec and file ops work even before the VM has
an IP and regardless of the guest's firewall.

## Why vsock
- Host↔guest only — nothing on the network can reach it.
- Available immediately at boot, before DHCP/networking.
- The same channel Firecracker recommends for host↔guest control.

## Protocol (Phase 1)
The host dials the guest's vsock port and sends one JSON request; the guest replies with a
stream of newline-delimited `ProcessEvent`s, mirroring `api/openapi.yaml` exactly:

```
host -> guest:  {"op":"exec","argv":["uname","-a"],"cwd":"/root","env":{},"pty":false}
guest -> host:  {"type":"stdout","data":"Linux ...\n"}
guest -> host:  {"type":"exit","code":0}
```

File ops reuse the same channel:
```
{"op":"list","path":"/root"}     -> [{"path":"...","kind":"file","size":123}, ...]
{"op":"read","path":"/root/x"}   -> raw bytes
{"op":"write","path":"/root/x"}  -> (bytes follow) -> {"ok":true}
```

## Status: implemented
- **Guest side:** `cmd/anviq-guest` — an AF_VSOCK listener (via `golang.org/x/sys/unix`, no
  third-party vsock lib) that serves exec, list, read, and write. Its exec-and-stream path is
  covered by offline tests (`cmd/anviq-guest/guest_test.go`) that run without KVM.
- **Host side:** `internal/vm/vsock.go` dials Firecracker's per-VM UDS with the `CONNECT`
  handshake; `internal/vm/exec.go` sends the request and forwards the guest's NDJSON stream.
- **Wiring:** `internal/vm/manager.go` attaches a vsock device (unique CID + per-VM UDS) to every
  microVM at boot. The wire types live in `internal/proto` (pure stdlib), shared by both sides.

## Remaining to pass `make smoke` (host-dependent, not code)
1. A KVM-capable Linux host (`/dev/kvm` present).
2. An uncompressed kernel at `/opt/anviq/vmlinux`.
3. A base rootfs at `/opt/anviq/base.ext4` with `/usr/local/bin/anviq-guest` installed and
   started at init.

## Build note
The agent is a static Go binary (`make guest`, i.e. `CGO_ENABLED=0 GOOS=linux`) copied into the
rootfs at `/usr/local/bin/anviq-guest` and started by the rootfs init. Static means it runs on
any minimal rootfs without libc matching.
