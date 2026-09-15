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

## Where it plugs in
`internal/vm/exec.go` is the host side of this protocol. It currently returns a loud
not-implemented exit; wiring the vsock client there and shipping this agent in the base image is
the last item to make `make smoke` pass.

## Build note
The agent is a static Go binary (`CGO_ENABLED=0`) copied into the rootfs at
`/usr/local/bin/anviq-guest` and started by the rootfs init (see `scripts/host-setup.sh`, which
builds the base image). Keeping it static means it runs on any minimal rootfs without libc
matching.
