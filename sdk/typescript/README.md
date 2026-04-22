# @jonnonz1/orchestrator-sdk (TypeScript)

Minimal TypeScript / JavaScript client for the [Orchestrator](../..) REST API. Works in Node 18+ and modern browsers via the built-in `fetch`.

## Install

```bash
npm install @jonnonz1/orchestrator-sdk
```

## Quick example

```ts
import { Client } from "@jonnonz1/orchestrator-sdk";

const orch = new Client({
  baseUrl: "http://127.0.0.1:8080",
  token: process.env.ORCHESTRATOR_AUTH_TOKEN,
});

const task = await orch.runTask({
  prompt: "Take a screenshot of https://example.com",
  ram_mb: 4096,
  timeout: 120,
});

// Stream output
for await (const chunk of orch.stream(task.id)) {
  process.stdout.write(chunk);
}

const final = await orch.wait(task.id);
console.log(`cost: $${final.cost_usd?.toFixed(4)}`);

for (const f of await orch.listFiles(task.id)) {
  const bytes = new Uint8Array(await orch.getFile(task.id, f.name));
  await import("node:fs/promises").then((fs) => fs.writeFile(f.name, bytes));
}
```

## License

Apache-2.0 (same as Orchestrator).
