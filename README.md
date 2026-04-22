<h1 align="center">Orchestrator</h1>

<p align="center">
  <strong>Self-hosted MicroVM orchestrator for AI agents.</strong><br/>
  KVM-isolated sandboxes for Claude Code, Aider, shell agents, and anything else you want to run untrusted.<br/>
  4-second cold boot · vsock-native · no Docker · no per-VM fee.
</p>

<p align="center">
  <a href="LICENSE"><img alt="License: Apache 2.0" src="https://img.shields.io/badge/license-Apache%202.0-blue.svg"></a>
  <a href="#"><img alt="Go 1.26+" src="https://img.shields.io/badge/go-1.26%2B-00ADD8.svg"></a>
  <a href="#status"><img alt="Status: alpha" src="https://img.shields.io/badge/status-alpha-orange.svg"></a>
</p>

---

## What is this?

Orchestrator spawns throwaway Firecracker MicroVMs on your own hardware and runs AI agents inside them. Each task gets a fresh kernel, fresh rootfs, fresh network namespace — and 4 seconds later it's ready to run arbitrary code, install packages, drive a browser, clone a repo, or anything else the agent can dream up. When the task finishes, the VM is shredded and the next one starts from the clean template.

The core use case is **Claude Code running Claude Code** — your top-level Claude delegates to sub-Claudes in isolated VMs over MCP. But the runtime is pluggable: you can point it at `aider`, `codex`, a custom shell script, or any binary in the rootfs.

## Why Orchestrator?

|                          | Orchestrator   | Docker        | E2B / SaaS    | SSH-based runners |
|--------------------------|----------------|---------------|---------------|-------------------|
| **Isolation**            | KVM (hardware) | Namespaces    | KVM           | None (shared host) |
| **Self-hosted**          | Yes            | Yes           | No            | Yes               |
| **Cold boot**            | ~4s            | ~1s           | ~1–3s         | instant           |
| **Per-task cost**        | Free           | Free          | Per-second    | Free              |
| **Agent auth injection** | Built-in       | Manual        | Manual        | Manual            |
| **Networking**           | Built-in NAT   | Bridge / host | Platform-managed | Host network   |
| **MCP server built-in**  | Yes            | No            | No            | No                |
| **Web UI built-in**      | Yes            | No            | Partial       | No                |

**You want Orchestrator if:** you have a Linux box with KVM, you're doing anything agentic, and you don't want to hand your credentials / source tree to a third party. **Use something else if:** you're running a single throwaway script (just use Docker), or you don't have root/KVM (use a SaaS sandbox).

## Status

**Alpha.** The happy path (create VM → run task → stream output → destroy) is solid on Ubuntu 24.04. Breaking changes may happen before 1.0. The guest auth surface (OAuth credential injection) means you should treat Orchestrator like a root daemon — don't expose it to untrusted networks without the bearer-token auth turned on.

## Quickstart

```bash
# 1. Install Firecracker + jailer + a guest kernel (requires sudo + KVM)
sudo make install-firecracker

# 2. Build the host binary, guest agent, and guest rootfs (~10 min first time)
make build
sudo make rootfs

# 3. Run a task
sudo ./bin/orchestrator task run \
  --prompt "Take a screenshot of https://example.com" \
  --ram 4096

# 4. Or start the server + dashboard
sudo ./bin/orchestrator serve        # REST + web UI at 127.0.0.1:8080
sudo ./bin/orchestrator mcp-serve    # MCP server at 127.0.0.1:8081
```

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│ HOST PROCESS (runs as root)                                  │
│                                                              │
│  REST API + Web UI         MCP Server           /metrics     │
│  (127.0.0.1:8080)          (127.0.0.1:8081)     (Prometheus)│
│         │                        │                            │
│         └──────────┬─────────────┘                           │
│                    │                                          │
│  ┌──── Task Runner ──── Runtime Registry ────┐               │
│  │       │                  ├── claude        │               │
│  │       │                  ├── shell         │               │
│  │       │                  └── (your plugin) │               │
│  │       │                                                    │
│  │       ├── VM Manager ──── jailer + firecracker            │
│  │       ├── Network Mgr ─── TAP + iptables NAT              │
│  │       ├── Stream Hub ──── WebSocket fan-out                │
│  │       ├── Event Sinks ─── webhook + audit log              │
│  │       └── Result Collector (files via vsock)               │
│  └───────────────────────────────────────────┘               │
└────────────────────────────────┬─────────────────────────────┘
                                 │ vsock (no network!)
┌────────────────────────────────▼─────────────────────────────┐
│ GUEST MicroVM (fresh per task, destroyed after)              │
│                                                              │
│  orchestrator-agent (systemd) ◄── vsock:9001                 │
│     │                                                         │
│     ├── exec (buffered + streaming)                          │
│     ├── write_files / read_file                              │
│     └── Spawns the agent runtime (claude, shell, …)          │
└──────────────────────────────────────────────────────────────┘
```

**Why vsock instead of SSH?** The kernel-to-kernel vsock channel is ready before guest networking is even up. Agent ping succeeds within milliseconds of the kernel booting. No SSH daemon, no keys, no network attack surface from the orchestrator's side.

## Example: Claude delegating to Claude

1. Start the MCP server: `sudo ./bin/orchestrator mcp-serve`
2. Add it to your Claude Code config:
   ```json
   { "mcpServers": { "orchestrator": {
     "type": "http",
     "url": "http://127.0.0.1:8081/mcp"
   }}}
   ```
3. Ask Claude anything that wants a sandbox:
   > *"Use orchestrator to clone torvalds/linux, count commits by author, and return a summary."*

Claude calls `run_task`, a fresh VM spins up, a sub-Claude does the work, files come back, VM gets shredded. Host Claude only sees the result.

## Running different agents

```bash
# Default — Anthropic Claude Code
sudo ./bin/orchestrator task run --prompt "..." --runtime claude

# Plain shell (no agent; useful for scripted work)
sudo ./bin/orchestrator task run --prompt "curl -sS https://example.com > /root/output/page.html" --runtime shell

# Your own runtime: implement internal/runtime.Runtime and register it.
```

## Security & authentication

- **Default bind is loopback (127.0.0.1).** To expose on the LAN, pass `--addr 0.0.0.0:8080`. When binding non-loopback, a bearer token is **required** — if you don't provide one via `--auth-token` or `ORCHESTRATOR_AUTH_TOKEN`, Orchestrator generates one on startup and prints it to stderr.
- **Agent auth**: Orchestrator ships with two modes — OAuth credentials copied from the host's `~/.claude/.credentials.json`, or `ANTHROPIC_API_KEY` injected as an env var (preferred for multi-user servers).
- **Guest permissions**: Claude inside the VM runs with `--dangerously-skip-permissions`. This is OK because the VM is a throwaway, but it means you should not use `--no-destroy` in production.
- **Network egress**: VMs get full outbound NAT by default. Locking down egress to an allowlist is on the roadmap.
- **Audit log**: set `ORCHESTRATOR_AUDIT_LOG=/var/log/orchestrator.jsonl` to get a JSON-lines record of every task.
- **Webhooks**: set `ORCHESTRATOR_WEBHOOK_URL` + `ORCHESTRATOR_WEBHOOK_SECRET` to receive HMAC-signed task lifecycle events.

See [SECURITY.md](SECURITY.md) for the full threat model and responsible disclosure policy.

## Observability

- **`/metrics`** — Prometheus text format (VM count, task count/duration histograms, bytes streamed).
- **Structured logs** — slog JSON via `ORCHESTRATOR_LOG_FORMAT=json`.
- **Web dashboard** — live streaming output, VM list, cost per task, file preview.

## Configuration

All runtime paths + server config are environment-variable overridable. See `orchestrator help` for the full list.

| Variable                        | Default                                | Description                                       |
|---------------------------------|----------------------------------------|---------------------------------------------------|
| `ORCHESTRATOR_FC_BASE`          | `/opt/firecracker`                     | Firecracker layout root                           |
| `ORCHESTRATOR_JAILER_BASE`      | `/srv/jailer/firecracker`              | Jailer chroot base                                |
| `ORCHESTRATOR_RESULTS_DIR`      | `$ORCHESTRATOR_FC_BASE/results`        | Downloaded task files                             |
| `ORCHESTRATOR_ADDR`             | `127.0.0.1:8080`                       | REST API + dashboard bind                         |
| `ORCHESTRATOR_MCP_ADDR`         | `127.0.0.1:8081`                       | MCP server bind                                   |
| `ORCHESTRATOR_AUTH_TOKEN`       | *(none)*                               | Bearer token for non-loopback HTTP                |
| `ORCHESTRATOR_AUDIT_LOG`        | *(disabled)*                           | JSON-lines audit log path                         |
| `ORCHESTRATOR_WEBHOOK_URL`      | *(disabled)*                           | HTTP URL to receive task events                   |
| `ORCHESTRATOR_WEBHOOK_SECRET`   | *(none)*                               | HMAC-SHA256 secret for webhook signatures         |
| `ANTHROPIC_API_KEY`      | *(OAuth fallback)*              | API key injected into the guest                   |

## SDKs

- [`sdk/python`](sdk/python) — Python client for the REST API
- [`sdk/typescript`](sdk/typescript) — TypeScript / Node client
- [OpenAPI spec](docs/openapi.yaml) — generate clients for any language

## Examples

See [`examples/`](examples) for sample prompts covering screenshots, repo analysis, build-and-screenshot, pentesting against dummies, and more.

## Performance (reference box: Ryzen 5 5600GT, 30 GB RAM)

| Metric             | Value                         |
|--------------------|-------------------------------|
| VM cold boot       | ~4 s                          |
| Agent ready        | <100 ms after boot            |
| Context injection  | <50 ms (vsock)                |
| End-to-end task    | 20–30 s (typical Claude task) |
| VM destroy         | <1 s                          |
| Concurrent VMs     | 8–12 (4 GB each)              |

## Documentation

Full hosted docs: **[jonnonz1.github.io/orchestrator](https://jonnonz1.github.io/orchestrator/)**

Or browse the markdown source under [`docs/`](docs/):

- [Quickstart](docs/getting-started/quickstart.md) · [Installation](docs/getting-started/installation.md) · [Configuration](docs/getting-started/configuration.md)
- [Architecture](docs/concepts/architecture.md) · [Security model](docs/concepts/security-model.md) · [Runtimes](docs/concepts/runtimes.md) · [Networking & vsock](docs/concepts/networking.md) · [Snapshots](docs/concepts/snapshots.md)
- [Running a task](docs/guides/running-tasks.md) · [Operating the server](docs/guides/operating-server.md) · [Deployment](docs/guides/deployment.md) · [Observability](docs/guides/observability.md) · [Egress policy](docs/guides/egress-policy.md) · [Rate limiting](docs/guides/rate-limiting.md) · [Troubleshooting](docs/guides/troubleshooting.md)
- Reference: [CLI](docs/reference/cli.md) · [Configuration](docs/reference/configuration.md) · [REST API](docs/reference/rest-api.md) · [MCP](docs/reference/mcp-server.md) · [Runtime plugin API](docs/reference/runtime-plugin.md) · [Firecracker deep-dive](docs/reference/firecracker.md) · [Technical internals](docs/reference/technical.md)
- [SDKs](docs/sdks/index.md) · [Examples](docs/examples.md) · [Threat model](SECURITY.md) · [FAQ](docs/faq.md)

## Contributing

Issues and PRs welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Good first issues labeled `good-first-issue`.

## License

[Apache License 2.0](LICENSE). Based on concepts from Anthropic's internal [AntSpace platform](https://aprilnea.me/en/blog/reverse-engineering-claude-code-antspace) (reverse-engineered writeup).

Originally built as three live-blogged episodes:
- [Part 1 — Claude Code Running Claude Code in 4-Second Disposable VMs](https://jonno.nz/posts/claude-code-running-claude-code-in-4-second-disposable-vms/)
- [Part 2 — 29 Hours Debugging iptables to Boot VMs in 4 Seconds](https://jonno.nz/posts/29-hours-debugging-iptables-to-boot-vms-in-4-seconds/)
- [Part 3 — Claude Code Can Now Spawn Copies of Itself in Isolated VMs](https://jonno.nz/posts/claude-code-can-now-spawn-copies-of-itself-in-isolated-vms/)
