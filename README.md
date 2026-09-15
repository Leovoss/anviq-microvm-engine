# anviq-microvm-engine

Anviq's sovereign execution engine: hardware-isolated [Firecracker](https://firecracker-microvm.github.io/)
microVM sandboxes, provisioned in sub-second cold-starts on flat-rate bare metal, exposed
behind a stable HTTP control-plane API.

It exists to solve the two problems every enterprise hits the moment it wants autonomous AI
agents that run real code:

1. **Cost.** Per-minute hosted sandbox bills are unpredictable and scale badly. A single
   bare-metal host runs dozens of microVMs at a flat, known cost.
2. **Isolation.** Shared containers are not a hard security boundary. A Firecracker microVM is
   a real KVM guest with its own kernel — the same isolation model AWS Lambda and Fargate use.

The engine speaks the same provider contract as [rakazo](https://github.com/elie222/rakazo)'s
sandbox layer, so it can back rakazo bots (or any client) as a drop-in `firecracker` computer
provider. See [`docs/rakazo-seam.md`](docs/rakazo-seam.md).

## Status

**Phase 1 — Plan A proof of concept (current).** Single bare-metal host, raw `firecracker`
processes, static host bridge + per-VM TAP, copy-on-write ext4 rootfs. Target: 1–10 teams.
This is the layer to prove the metal before adding orchestration.

Phase 2 (Plan B: `jailer`, cgroups v2, CNI, devmapper snapshots, multi-host fleet) and Phase 3
(Rostock-class sovereign enterprise deployment) are designed but not built. See
[`docs/roadmap.md`](docs/roadmap.md) and [`docs/architecture.md`](docs/architecture.md).

## What is here

```
docs/architecture.md   Plan A (now) and Plan B (later), with the boundary between them
docs/rakazo-seam.md     How the control-plane API maps 1:1 onto rakazo's SandboxProvider
docs/roadmap.md         Plan A PoC -> Plan B fleet -> Rostock enterprise, with exit criteria
api/openapi.yaml        The control-plane HTTP contract (source of truth for both sides)
cmd/fcctl/             Control-plane daemon entrypoint
internal/vm/           Firecracker orchestration: lifecycle, networking, rootfs
internal/server/       HTTP handlers implementing api/openapi.yaml
internal/guest/        Guest-agent exec protocol (runs inside the microVM)
scripts/host-setup.sh   One-time host prep: KVM check, bridge, NAT, kernel + rootfs fetch
adapter/               Reference rakazo SandboxProvider adapter (TypeScript, drop-in)
```

## Non-negotiables (carried from Anviq's positioning)

- **No outbound dependency for the core.** The engine runs entirely on-prem. No control-plane
  call leaves the host; no US hyperscaler is in the data path. This is what makes it viable for
  German public-sector / BSI / GDPR deployments.
- **Provider-neutral seam.** The control plane knows nothing about rakazo, models, or LLM
  vendors. It provisions and runs microVMs. Everything above it is a client.
- **Incremental verification.** Every layer boots and is verified before the next is added.
  `make smoke` boots one microVM and runs a command inside it — that is the whole Phase 1 bar.

## Quick start (Plan A, on a KVM-capable Linux host)

```bash
# 1. One-time host prep (needs root: bridge, NAT, kernel + rootfs into /opt/anviq)
sudo ./scripts/host-setup.sh

# 2. Build and run the control plane
make build
sudo ./bin/fcctl --listen 127.0.0.1:8080 --state-dir /var/lib/anviq

# 3. Boot a microVM and run a command inside it
curl -sX POST localhost:8080/v1/sandboxes -d '{"team_id":"demo","vcpus":2,"mem_mib":2048}'
# -> {"id":"vm-demo-...", ...}
curl -sX POST localhost:8080/v1/sandboxes/<id>/exec -d '{"argv":["uname","-a"]}'
```

`fcctl` requires KVM (`/dev/kvm`) and root (or `CAP_NET_ADMIN` + KVM access) on the host. It
does **not** run inside an unprivileged container.

## License

Proprietary. All rights reserved. This is Anviq infrastructure, not open source.
