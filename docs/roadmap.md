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

## Phase 3 — Rostock-class sovereign enterprise  ·  ~2,000 users

The compliance layer that wins German public-sector deals. This is packaging and proof, on top
of a Phase 2 engine.

- [ ] **Air-gapped deploy:** Helm chart / K8s manifests that install with no outbound pulls;
      all images mirrored into the university registry.
- [ ] **Identity:** SSO via Shibboleth / DFN-AAI / LDAP; role-based access enforced by the
      client (rakazo Spaces), microVM boundary enforced by the engine.
- [ ] **Encryption:** BYOK — university holds exclusive keys; rootfs and snapshots encrypted at
      rest with keys the operator controls.
- [ ] **Local models:** LLM + embedding weights served on-prem; verify zero outbound API calls
      in the whole data path (this repo already guarantees the engine half).
- [ ] **Auditability:** structured, immutable audit log of every sandbox lifecycle event; local
      backup; documentation mapped to BSI C5 / DSGVO / DSG M-V controls.

**Exit criterion:** a data-protection officer and ITMZ can run the full stack air-gapped on
university hardware, verify no data crosses the EU boundary, and audit every action — from a
single documented Helm install.

### Open questions to confirm with the Rostock contact before Phase 3 design
1. On-prem bare-metal/VM, or an accepted German sovereign cloud (SCS / certified DC)?
2. Data classification of the 2,000 users' workloads: public research, internal admin, or
   student PII? (PII triggers the higher compliance tier and changes the audit requirements.)
3. Which identity federation does ITMZ mandate — Shibboleth, DFN-AAI, plain LDAP?
