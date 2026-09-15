// Reference rakazo SandboxProvider for the Anviq microVM control plane.
//
// This file lives in the engine repo, NOT in rakazo — rakazo stays untouched until you
// decide to wire it in. When you do, it drops into `packages/adapters/src/` with three
// small companion edits (see docs/rakazo-seam.md):
//   1. add "firecracker" to SandboxKind in packages/contracts/src/ids.ts
//   2. add a `case "firecracker"` in packages/adapters/src/sandbox-factory.ts
//   3. export this provider from packages/adapters/src/index.ts
//
// It targets the same interface the Daytona/Box adapters implement, so the shapes below
// are deliberately faithful to `@rakazo/adapter-kit`. Types are imported with top-level
// `import type` per rakazo's AGENTS.md. The provider takes a GENERIC connection
// (base URL + bearer token), never Firecracker-specific env vars.

import type {
  AdapterContext,
  CommandRequest,
  ComputerRef,
  PortableFile,
  ProcessEvent,
  SandboxProvider,
  SnapshotRef,
} from "@rakazo/adapter-kit";

interface ControlPlaneSandbox {
  id: string;
  team_id: string;
  state: "booting" | "running" | "paused" | "destroyed";
  ip: string;
  fresh?: boolean;
}

export interface FirecrackerConfig {
  /** Control-plane base URL, e.g. http://10.0.0.5:8080 (private subnet / loopback). */
  baseUrl: string;
  /** Host-to-host bearer token shared with fcctl (ANVIQ_CONTROL_TOKEN). */
  token: string;
}

export class FirecrackerSandboxProvider implements SandboxProvider {
  constructor(private readonly cfg: FirecrackerConfig) {}

  describe() {
    return {
      id: "firecracker",
      contractVersion: "1",
      adapterVersion: "0.1.0",
      capabilities: {
        graphical: false, // Phase 1: no desktop yet — degrade safely rather than fake it.
        pty: true,
        snapshots: true,
        takeover: false,
        persistentHome: true,
      },
    };
  }

  async provision(
    request: { botId: string; homePath: string; providerRef?: string; providerKind?: ComputerRef["kind"] },
    context: AdapterContext,
  ): Promise<ComputerRef> {
    // Reconnect an existing microVM by ref (survives a rakazo restart), else create fresh.
    if (request.providerRef && request.providerKind === "firecracker") {
      const existing = await this.fetch(`/v1/sandboxes/${request.providerRef}`, context).catch(() => null);
      if (existing?.ok) {
        const box = (await existing.json()) as ControlPlaneSandbox;
        return this.toRef(box, request.botId, false);
      }
    }
    const res = await this.fetch("/v1/sandboxes", context, {
      method: "POST",
      body: JSON.stringify({ team_id: context.spaceId, vcpus: 2, mem_mib: 2048 }),
    });
    const box = (await res.json()) as ControlPlaneSandbox;
    return this.toRef(box, request.botId, true);
  }

  async prepare(computer: ComputerRef, context: AdapterContext): Promise<void> {
    await this.fetch(`/v1/sandboxes/${computer.providerRef}/prepare`, context, { method: "POST" });
  }

  async *execute(
    computer: ComputerRef,
    request: CommandRequest,
    context: AdapterContext,
  ): AsyncIterable<ProcessEvent> {
    const res = await this.fetch(`/v1/sandboxes/${computer.providerRef}/exec`, context, {
      method: "POST",
      body: JSON.stringify({
        argv: request.argv,
        cwd: request.cwd,
        env: request.env,
        pty: request.pty,
        timeout_ms: request.timeoutMs,
      }),
    });
    // The control plane streams newline-delimited ProcessEvents; forward them as-is.
    const reader = res.body?.getReader();
    if (!reader) return;
    const decoder = new TextDecoder();
    let buffer = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let nl: number;
      while ((nl = buffer.indexOf("\n")) >= 0) {
        const line = buffer.slice(0, nl).trim();
        buffer = buffer.slice(nl + 1);
        if (line) yield JSON.parse(line) as ProcessEvent;
      }
    }
  }

  async snapshot(computer: ComputerRef, context: AdapterContext): Promise<SnapshotRef> {
    const res = await this.fetch(`/v1/sandboxes/${computer.providerRef}/snapshot`, context, {
      method: "POST",
    });
    const body = (await res.json()) as { id: string; created_at: string };
    return { id: body.id, createdAt: body.created_at };
  }

  async keepAlive(computer: ComputerRef): Promise<void> {
    await fetch(`${this.cfg.baseUrl}/v1/sandboxes/${computer.providerRef}/keepalive`, {
      method: "POST",
      headers: { authorization: `Bearer ${this.cfg.token}` },
    });
  }

  async stop(computer: ComputerRef, context: AdapterContext): Promise<void> {
    await this.fetch(`/v1/sandboxes/${computer.providerRef}/stop`, context, { method: "POST" });
  }

  async destroy(computer: ComputerRef, context: AdapterContext): Promise<void> {
    await this.fetch(`/v1/sandboxes/${computer.providerRef}`, context, { method: "DELETE" });
  }

  // --- File ops (served by the in-guest agent over the control plane) ---

  async listFiles(computer: ComputerRef, path: string, context: AdapterContext) {
    const res = await this.fetch(
      `/v1/sandboxes/${computer.providerRef}/fs?mode=list&path=${encodeURIComponent(path)}`,
      context,
    );
    return (await res.json()) as Array<{ path: string; kind: "file" | "dir"; size: number }>;
  }

  async readFile(computer: ComputerRef, path: string, context: AdapterContext): Promise<Uint8Array> {
    const res = await this.fetch(
      `/v1/sandboxes/${computer.providerRef}/fs?mode=read&path=${encodeURIComponent(path)}`,
      context,
    );
    return new Uint8Array(await res.arrayBuffer());
  }

  async writeFile(computer: ComputerRef, file: PortableFile, context: AdapterContext): Promise<void> {
    await this.fetch(
      `/v1/sandboxes/${computer.providerRef}/fs?path=${encodeURIComponent(file.path)}`,
      context,
      { method: "PUT", body: file.content },
    );
  }

  // --- helpers ---

  private toRef(box: ControlPlaneSandbox, botId: string, fresh: boolean): ComputerRef {
    return { id: box.id, botId, kind: "firecracker", providerRef: box.id, fresh };
  }

  private fetch(path: string, context: AdapterContext, init: RequestInit = {}): Promise<Response> {
    return fetch(`${this.cfg.baseUrl}${path}`, {
      ...init,
      signal: context.signal, // forward rakazo's abort so cancels tear down the request
      headers: {
        authorization: `Bearer ${this.cfg.token}`,
        "content-type": "application/json",
        ...(init.headers ?? {}),
      },
    }).then((res) => {
      if (!res.ok && res.status !== 404) {
        throw new Error(`firecracker control plane ${path}: ${res.status}`);
      }
      return res;
    });
  }
}
