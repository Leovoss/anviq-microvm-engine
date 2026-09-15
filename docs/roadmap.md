# Roadmap

Three phases. Each has a single, testable exit criterion — you do not move on until it is met.
This is the "validate every layer incrementally" discipline made concrete.

## Phase 1 — Plan A proof of concept  ·  1–10 teams

The bar is deliberately small and physical.

- [ ] `scripts/host-setup.sh` brings a KVM host from bare to ready: verifies `/dev/kvm`, creates
      `br0` + NAT, fetches a kernel (`vmlinux`) and base rootfs into `/opt/anviq`.
- [ ] `fcctl` boots one microVM via `POST /v1/sandboxes` and returns a `ComputerRef`-shaped body.
- [ ] `POST /v1/sandboxes/{id}/exec` runs `uname -a` inside the VM and streams the output back.
- [ ] `stop` pauses + snapshots; a later `provision` on the same `providerRef` resumes it.
- [ ] `destroy` leaves no orphan TAP, overlay, or firecracker process.
- [ ] Reference rakazo adapter (`adapter/firecracker-sandbox.ts`) passes rakazo's
      `sandbox-conformance.test.ts` against a local `fcctl`.

**Exit criterion:** `make smoke` boots a microVM, runs a command, and tears it down cleanly,
repeatedly, on one host. That is the entire moat's foundation working end to end.

**Deliberately out of scope for Phase 1:** graphical desktop, multi-host, jailer, cgroups,
autoscaling, a UI. Adding any of these before the smoke test passes is the failure mode.

## Phase 2 — Plan B fleet  ·  100–2,000+ teams

Only starts once Phase 1's exit criterion holds under sustained use (e.g. 10 VMs up for a week).

- [ ] Wrap `firecracker` in `jailer` (chroot, uid/gid drop, dedicated netns per VM).
- [ ] cgroups v2 enforcement: `cpu.max`, `memory.max`, `io.max` per VM; verify no noisy-neighbour
      degradation under load.
- [ ] Replace static bridge/TAP with CNI `tc-redirect-tap` + a cross-host overlay
      (WireGuard or VXLAN).
- [ ] devmapper thin snapshots for instant rootfs at high VM density.
- [ ] Fleet scheduler: place a new VM on the least-loaded host; drain and reschedule on host loss.
- [ ] Load target: 500 concurrent microVMs across ≥3 hosts, p99 cold-start under target.

**Exit criterion:** the same API and adapter from Phase 1 serve 500+ concurrent isolated VMs
across multiple hosts, with per-tenant resource caps enforced — no client-visible change.

## Phase 3 — Codename ROSTOCK: the hardened / sovereign tier

"Rostock" is the internal codename for the hardened deployment profile — the set of controls
that lets the engine run for a client (or for yourself) with strict isolation, data-sovereignty,
and audit requirements. It is not a specific customer; it is the generic "locked-down" build on
top of a proven Phase 2 engine. Any deployment that needs air-gapping and audit turns this
profile on.

- [ ] **Air-gapped deploy:** Helm chart / K8s manifests (or a plain systemd bundle) that install
      with no outbound pulls; all images mirrored into the operator's own registry.
- [ ] **Identity:** SSO via OIDC / SAML / LDAP; role-based access enforced by the client
      (e.g. rakazo Spaces), the microVM boundary enforced by the engine.
- [ ] **Encryption:** BYOK — the operator holds exclusive keys; rootfs and snapshots encrypted at
      rest with keys they control.
- [ ] **Local models:** LLM + embedding weights served on-prem; verify zero outbound API calls in
      the whole data path (this repo already guarantees the engine half).
- [ ] **Auditability:** structured, immutable audit log of every sandbox lifecycle event; local
      backup; controls documented against a recognised framework (SOC 2 / ISO 27001 / BSI C5,
      whichever the client requires).

**Exit criterion:** an operator can run the full stack air-gapped on their own hardware, verify
no data leaves their boundary, and audit every action — from a single documented install.

### Questions that shape a ROSTOCK deployment (per client, when one appears)
1. On-prem bare-metal/VM, or a sovereign/certified cloud?
2. Data sensitivity of the workloads? (Higher sensitivity → stricter audit + key custody.)
3. Which identity system must it federate with — OIDC, SAML, or plain LDAP?
