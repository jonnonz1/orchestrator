# TypeScript SDK

Source: [`sdk/typescript/`](https://github.com/jonnonz1/orchestrator/tree/main/sdk/typescript).

```bash
npm install @jonnonz1/orchestrator-sdk
```

ESM only (`"type":"module"`). Works in Node 18+ and any modern browser with
`fetch` + `WebSocket` + `ReadableStream`.

## Quick example

```ts
import { Client } from "@jonnonz1/orchestrator-sdk";

const client = new Client({
  baseUrl: "http://127.0.0.1:8080",
  token: "your-bearer-token", // optional on loopback
});

const task = await client.runTask({
  prompt: "Take a screenshot of https://example.com",
  ram_mb: 4096,
  timeout: 120,
});

// Stream over WebSocket
for await (const event of client.streamTask(task.id)) {
  process.stdout.write(event.data);
}

// Or await completion
const final = await client.waitForTask(task.id);
console.log(`Cost: $${final.cost_usd?.toFixed(4)}`);

// Download result files
const files = await client.listFiles(task.id);
for (const f of files) {
  const blob = await client.getFile(task.id, f.name);
  // … write to disk / upload / inline in UI …
}
```

## Client options

```ts
interface ClientOptions {
  baseUrl: string;      // e.g. "http://127.0.0.1:8080"
  token?: string;       // bearer token for non-loopback servers
  fetch?: typeof fetch; // injection point for Node <18 (node-fetch) or tests
}
```

## Methods

### VMs

- `listVMs()`
- `createVM({ name, ram_mb, vcpus })`
- `destroyVM(name)`

### Tasks

- `runTask(options)` — POSTs `/api/v1/tasks` and returns the `Task` (202 Accepted).
- `getTask(id)`, `listTasks()`, `cancelTask(id)`
- `waitForTask(id, { pollIntervalMs, timeoutMs })` — polling loop
- `streamTask(id)` — async iterator over WebSocket events; auto-includes
  `?token=<token>` when a bearer token is configured.

### Files

- `listFiles(id)` returns `{ name, url, size }[]`
- `getFile(id, filename)` returns a `Blob` (browser) or `Buffer` (Node).

## Browser vs Node

The SDK is platform-agnostic. On browsers, `fetch` + `WebSocket` are global;
on Node 18+ they're globals too. For older Node, inject `node-fetch` via
`{ fetch: fetch as typeof globalThis.fetch }` and bring your own WS
(unsupported).

## Types

```ts
interface Task {
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

interface VM {
  name: string;
  pid: number;
  ram_mb: number;
  vcpus: number;
  guest_ip: string;
  state: string;
}
```

Full list is in [`sdk/typescript/src/index.ts`](https://github.com/jonnonz1/orchestrator/blob/main/sdk/typescript/src/index.ts).
