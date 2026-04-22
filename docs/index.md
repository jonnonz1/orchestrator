---
hide:
  - navigation
  - toc
---

# Orchestrator

<p style="font-size:1.25rem; color: var(--md-default-fg-color--light); max-width: 720px;">
  Self-hosted MicroVM orchestrator for AI agents. KVM-isolated Firecracker
  sandboxes for Claude Code, Aider, shell agents, and anything else you want
  to run untrusted. 4-second cold boot · vsock-native · no Docker · no per-VM
  fee.
</p>

<div class="grid cards" markdown>

-   :material-rocket-launch-outline: **Quickstart**

    ---

    Install Firecracker, build the binary, run your first task in ten minutes.

    [:octicons-arrow-right-24: Getting started](getting-started/quickstart.md)

-   :material-cpu-64-bit: **How it works**

    ---

    Architecture, security model, runtimes, networking, snapshots.

    [:octicons-arrow-right-24: Concepts](concepts/index.md)

-   :material-book-open-variant: **Reference**

    ---

    CLI, REST API, MCP tools, configuration, runtime plugin API.

    [:octicons-arrow-right-24: Reference](reference/index.md)

-   :material-shield-lock: **Security**

    ---

    Threat model, hardening checklist, responsible disclosure.

    [:octicons-arrow-right-24: Security](security.md)

</div>

## What is Orchestrator?

Orchestrator spawns throwaway Firecracker MicroVMs on your own hardware and
runs AI agents inside them. Each task gets a fresh kernel, fresh rootfs, fresh
network namespace — and 4 seconds later it's ready to run arbitrary code,
install packages, drive a browser, clone a repo, or anything else the agent
can dream up. When the task finishes, the VM is shredded and the next one
starts from the clean template.

The core use case is **Claude Code running Claude Code** — your top-level
Claude delegates to sub-Claudes in isolated VMs over MCP. But the runtime is
pluggable: you can point it at `aider`, `codex`, a custom shell script, or any
binary in the rootfs.

## Why Orchestrator?

|                          | Orchestrator   | Docker        | E2B / SaaS    | SSH runners |
|--------------------------|----------------|---------------|---------------|-------------|
| **Isolation**            | KVM (hardware) | Namespaces    | KVM           | None        |
| **Self-hosted**          | Yes            | Yes           | No            | Yes         |
| **Cold boot**            | ~4 s           | ~1 s          | ~1–3 s        | instant     |
| **Per-task cost**        | Free           | Free          | Per-second    | Free        |
| **Agent auth injection** | Built-in       | Manual        | Manual        | Manual      |
| **Networking**           | Built-in NAT   | Bridge / host | Managed       | Host        |
| **MCP server built-in**  | Yes            | No            | No            | No          |
| **Web UI built-in**      | Yes            | No            | Partial       | No          |

**You want Orchestrator if** you have a Linux box with KVM, you're doing
anything agentic, and you don't want to hand your credentials or source tree
to a third party. **Use something else** if you're running a single throwaway
script (just use Docker), or you don't have root or KVM (use a SaaS sandbox).

## 30-second demo

```bash
# 1. Install Firecracker + jailer + a guest kernel (one time, requires sudo + KVM)
sudo make install-firecracker

# 2. Build the host binary, guest agent, and guest rootfs (~10 min first time)
make build
sudo make rootfs

# 3. Run a task
sudo ./bin/orchestrator task run \
  --prompt "Take a screenshot of https://example.com" \
  --ram 4096
```

Or start the server + dashboard:

```bash
sudo ./bin/orchestrator serve         # REST API + embedded dashboard
sudo ./bin/orchestrator mcp-serve     # MCP server for Claude Code clients
```

## Status

**Alpha.** The happy path (create VM → run task → stream output → destroy) is
solid on Ubuntu 24.04. Breaking changes may happen before 1.0. The guest auth
surface (OAuth credential injection) means you should treat Orchestrator like
a root daemon — don't expose it to untrusted networks without the bearer-token
auth turned on.

## Key features

- **KVM isolation** — real Firecracker MicroVMs, not container namespaces.
- **vsock-native** — agent ready before guest networking even comes up; no SSH daemon, no keys, no network attack surface.
- **Pluggable runtimes** — ships `claude` and `shell`; add `aider`, `codex`, or your own in ~50 lines.
- **MCP server built-in** — Claude Code delegates to sub-Claudes over stdio or HTTP.
- **Web dashboard** — embedded React UI with live streaming, VM management, cost tracking, file preview.
- **Production knobs** — bearer auth, Prometheus metrics, webhook events, audit log, egress allowlist, rate limits.
- **SDKs + OpenAPI** — Python + TypeScript clients, or generate for any language from the OpenAPI spec.
