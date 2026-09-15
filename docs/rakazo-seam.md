# The rakazo seam

The engine is designed to back rakazo bots without rakazo knowing anything Firecracker-specific.
This document is the contract: it maps rakazo's `SandboxProvider` interface onto the
control-plane HTTP API 1:1, so the two halves fit from day one.

## Where the seam lives in rakazo

rakazo defines every computer backend (Docker, E2B, Daytona, Box) as an implementation of one
interface:

- Interface: `SandboxProvider` in `packages/adapter-kit/src/interfaces.ts`
- Wired up in: `packages/adapters/src/sandbox-factory.ts` (`createSandboxProvider`)
- Provider identity: `SandboxKind` in `packages/contracts/src/ids.ts`
  (`"docker" | "e2b" | "daytona" | "box" | "desktop" | "fake"`)

Adding Firecracker to rakazo later is three edits, none of which we make now (rakazo stays
untouched until you decide):

1. Add `"firecracker"` to the `SandboxKind` enum.
2. Add a `FirecrackerSandboxProvider implements SandboxProvider` under `packages/adapters/src/`
   (reference implementation lives in [`adapter/firecracker-sandbox.ts`](../adapter/firecracker-sandbox.ts)).
3. Add a `case "firecracker"` in `createSandboxProvider`, taking the control-plane URL + token.

Per rakazo's `AGENTS.md`: the provider takes a **generic connection** (base URL + bearer token),
not Firecracker-specific env vars. The control plane is "just another remote sandbox provider,"
the same shape as Daytona/Box.

## Method → endpoint mapping

`ComputerRef.providerRef` carries the microVM id. `AdapterContext` carries `spaceId` / `botId` /
`signal`; the adapter forwards the abort signal by cancelling the HTTP request.

| `SandboxProvider` method | Control-plane call | Notes |
|---|---|---|
| `describe()` | (static) | Returns `SandboxCapabilities`. Phase 1: `{pty:true, snapshots:true, persistentHome:true, graphical:false, takeover:false}`. Graphical degrades safely off until Phase 2. |
| `provision({botId, homePath, providerRef?})` | `POST /v1/sandboxes` or `GET /v1/sandboxes/{ref}` | If `providerRef` + `kind==="firecracker"`, reconnect (GET); on 404 create fresh and set `ComputerRef.fresh=true`. Returns before fallible setup, per the interface contract. |
| `prepare(computer)` | `POST /v1/sandboxes/{id}/prepare` | Idempotent guest setup (workspace dir, tools). Safe to call repeatedly. |
| `execute(computer, {argv,cwd,env,pty,timeoutMs})` | `POST /v1/sandboxes/{id}/exec` (streaming) | Response is newline-delimited JSON `ProcessEvent`s: `{"type":"stdout"|"stderr","data":...}` then `{"type":"exit","code":n}`. Maps directly to rakazo's `AsyncIterable<ProcessEvent>`. |
| `listFiles / readFile / writeFile` | `GET/PUT /v1/sandboxes/{id}/fs?path=` | Served by the in-guest agent over the host vsock/API. |
| `exportWorkspace / importWorkspace` | `GET/POST /v1/sandboxes/{id}/workspace` | Streams `PortableFile`s (tar over the wire). Backs bot state portability. |
| `snapshot(computer)` | `POST /v1/sandboxes/{id}/snapshot` | Firecracker full snapshot (memory + disk). Returns `SnapshotRef {id, createdAt}`. |
| `keepAlive?(computer)` | `POST /v1/sandboxes/{id}/keepalive` | Resets the idle-release timer. |
| `stop(computer)` | `POST /v1/sandboxes/{id}/stop` | Pause/suspend: VM state persists, resources released. `provision` later resumes from snapshot. |
| `destroy(computer)` | `DELETE /v1/sandboxes/{id}` | Tear down: kill VM, drop TAP, delete CoW overlay. Irreversible. |
| `connectScreen / observe / act / sendInput` | — (Phase 2) | Graphical desktop. Phase 1 reports `graphical:false` so rakazo degrades safely instead of pretending. |

## Lifecycle contract (must match rakazo's expectations)

- **`provision` returns fast, before setup can fail.** rakazo persists the `ComputerRef` first,
  then calls `prepare`. This lets rakazo recover a half-provisioned computer instead of leaking
  it. The control plane mirrors this: `POST /v1/sandboxes` reserves the id + boots the VM shell,
  `prepare` does the fallible guest setup.
- **Everything is keyed on `providerRef` (the microVM id),** so a rakazo restart can reconnect
  by GET without re-creating state — the same way the Daytona adapter reconnects by sandbox id.
- **`stop` is reversible, `destroy` is not.** rakazo's idle manager calls `stop`; only explicit
  deletion calls `destroy`. Phase 1 implements `stop` as a Firecracker pause + snapshot so a
  resumed VM is byte-identical.

## Auth

One bearer token, host-to-host, over loopback or a private subnet. The control plane never
authenticates end users — that is rakazo's job (Spaces, Better Auth). The engine trusts its one
caller and isolates tenants by microVM boundary, not by request identity.
