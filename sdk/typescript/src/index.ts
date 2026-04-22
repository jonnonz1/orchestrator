/**
 * Orchestrator TypeScript SDK.
 *
 * Works in Node 18+ and modern browsers (fetch + ReadableStream).
 */

export interface ClientOptions {
  baseUrl: string;
  token?: string;
  fetch?: typeof fetch;
}

export interface Task {
  id: string;
  status: "pending" | "running" | "completed" | "failed" | "cancelled";
  prompt: string;
  runtime?: string;
  vm_name?: string;
  ram_mb: number;
  vcpus: number;
  exit_code?: number;
  output?: string;
  result_files?: string[];
  cost_usd?: number;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  error?: string;
}

export interface VM {
  name: string;
  pid: number;
  ram_mb: number;
  vcpus: number;
  guest_ip: string;
  state: string;
}

export interface RunTaskOptions {
  prompt: string;
  runtime?: string;
  ram_mb?: number;
  vcpus?: number;
  timeout?: number;
  max_turns?: number;
}

export interface FileEntry {
  name: string;
  size: number;
  mime_type?: string;
}

export class OrchestratorError extends Error {
  constructor(message: string, public status = 0, public body = "") {
    super(message);
    this.name = "OrchestratorError";
  }
}

export class Client {
  private baseUrl: string;
  private token?: string;
  private fetchImpl: typeof fetch;

  constructor(opts: ClientOptions) {
    this.baseUrl = opts.baseUrl.replace(/\/+$/, "");
    this.token = opts.token;
    this.fetchImpl = opts.fetch ?? globalThis.fetch;
    if (!this.fetchImpl) {
      throw new OrchestratorError("no fetch available — pass { fetch } explicitly");
    }
  }

  listVMs(): Promise<VM[]> {
    return this.request("GET", "/api/v1/vms");
  }

  createVM(name: string, ram_mb = 2048, vcpus = 2): Promise<VM> {
    return this.request("POST", "/api/v1/vms", { name, ram_mb, vcpus });
  }

  destroyVM(name: string): Promise<void> {
    return this.request("DELETE", `/api/v1/vms/${encodeURIComponent(name)}`);
  }

  runTask(opts: RunTaskOptions): Promise<Task> {
    return this.request("POST", "/api/v1/tasks", {
      runtime: "claude",
      ram_mb: 2048,
      vcpus: 2,
      timeout: 600,
      ...opts,
    });
  }

  getTask(id: string): Promise<Task> {
    return this.request("GET", `/api/v1/tasks/${id}`);
  }

  listTasks(): Promise<Task[]> {
    return this.request("GET", "/api/v1/tasks");
  }

  cancelTask(id: string): Promise<void> {
    return this.request("DELETE", `/api/v1/tasks/${id}`);
  }

  async wait(id: string, pollMs = 1000, timeoutMs = 600_000): Promise<Task> {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const t = await this.getTask(id);
      if (t.status === "completed" || t.status === "failed" || t.status === "cancelled") return t;
      await new Promise((r) => setTimeout(r, pollMs));
    }
    throw new OrchestratorError(`task ${id} did not finish within ${timeoutMs}ms`);
  }

  listFiles(id: string): Promise<FileEntry[]> {
    return this.request("GET", `/api/v1/tasks/${id}/files`);
  }

  async getFile(id: string, filename: string): Promise<ArrayBuffer> {
    const resp = await this.rawFetch("GET", `/api/v1/tasks/${id}/files/${encodeURIComponent(filename)}`);
    if (!resp.ok) throw new OrchestratorError(`GET file → ${resp.status}`, resp.status, await resp.text());
    return await resp.arrayBuffer();
  }

  async *stream(id: string, pollMs = 500): AsyncGenerator<string> {
    let seen = 0;
    while (true) {
      const t = await this.getTask(id);
      const out = t.output ?? "";
      if (out.length > seen) {
        yield out.slice(seen);
        seen = out.length;
      }
      if (t.status === "completed" || t.status === "failed" || t.status === "cancelled") return;
      await new Promise((r) => setTimeout(r, pollMs));
    }
  }

  // ---- internals ----

  private async rawFetch(method: string, path: string, body?: unknown): Promise<Response> {
    const headers: Record<string, string> = { Accept: "application/json" };
    if (this.token) headers.Authorization = `Bearer ${this.token}`;
    let payload: BodyInit | undefined;
    if (body !== undefined) {
      headers["Content-Type"] = "application/json";
      payload = JSON.stringify(body);
    }
    return this.fetchImpl(this.baseUrl + path, { method, headers, body: payload });
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const resp = await this.rawFetch(method, path, body);
    if (!resp.ok) {
      throw new OrchestratorError(`${method} ${path} → ${resp.status}`, resp.status, await resp.text());
    }
    const text = await resp.text();
    if (!text) return undefined as T;
    try {
      return JSON.parse(text) as T;
    } catch {
      return text as unknown as T;
    }
  }
}
