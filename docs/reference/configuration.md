# Configuration (environment variables)

All runtime config is environment-variable-driven. The orchestrator reads
everything on process startup; changes require a restart unless noted.

Group quick-jump: [Paths](#paths) · [Server](#server) · [Auth / CORS](#auth--cors) ·
[Observability](#observability) · [Guardrails](#guardrails) · [Runtime](#runtime)

## Paths

| Variable | Default | Meaning |
|---|---|---|
| `ORCHESTRATOR_FC_BASE` | `/opt/firecracker` | Root. All other path defaults derive from this. |
| `ORCHESTRATOR_FC_BIN` | `$FC_BASE/firecracker` | Firecracker binary. |
| `ORCHESTRATOR_JAILER_BIN` | `$FC_BASE/jailer` | Jailer binary. |
| `ORCHESTRATOR_KERNEL` | `$FC_BASE/kernels/vmlinux` | Guest kernel image. |
| `ORCHESTRATOR_BASE_ROOTFS` | `$FC_BASE/rootfs/base-rootfs.ext4` | Template rootfs (copied per-VM). |
| `ORCHESTRATOR_VM_DIR` | `$FC_BASE/vms` | Per-VM metadata + rootfs copies. |
| `ORCHESTRATOR_JAILER_BASE` | `/srv/jailer/firecracker` | Jailer chroot base (not under FC_BASE; matches jailer's assumed default). |
| `ORCHESTRATOR_RESULTS_DIR` | `$FC_BASE/results` | Task result files after download from guest. |

## Server

| Variable | Default | Meaning |
|---|---|---|
| `ORCHESTRATOR_ADDR` | `127.0.0.1:8080` | REST API + dashboard bind. |
| `ORCHESTRATOR_MCP_ADDR` | `127.0.0.1:8081` | MCP (Streamable-HTTP) bind. |

## Auth / CORS

| Variable | Default | Meaning |
|---|---|---|
| `ORCHESTRATOR_AUTH_TOKEN` | *(unset)* | Bearer token. Required on non-loopback binds (auto-generated and printed if absent). Empty on loopback = no auth. |
| `ORCHESTRATOR_CORS_ORIGINS` | *(unset)* | Comma-separated origins allowed for CORS. Unset = same-origin only (no CORS middleware installed). Including `*` disables credentials. |

## Observability

| Variable | Default | Meaning |
|---|---|---|
| `ORCHESTRATOR_AUDIT_LOG` | *(unset)* | Path to JSON-lines audit log. Append-only, 0600 perms. Unset = disabled. |
| `ORCHESTRATOR_WEBHOOK_URL` | *(unset)* | http/https URL that receives task-lifecycle events. Other schemes rejected. |
| `ORCHESTRATOR_WEBHOOK_SECRET` | *(unset)* | HMAC-SHA256 secret for `X-Orchestrator-Signature`. |
| `ORCHESTRATOR_LOG_FORMAT` | `text` | `text` or `json`. |

## Guardrails

| Variable | Default | Meaning |
|---|---|---|
| `ORCHESTRATOR_MAX_CONCURRENT_VMS` | *(unlimited)* | Semaphore on simultaneous running VMs. Exceeding returns 503. |
| `ORCHESTRATOR_TASK_RATE_LIMIT` | *(unlimited)* | Token bucket, tasks per minute. Exceeding returns 429 with `Retry-After: 60`. |
| `ORCHESTRATOR_EGRESS_ALLOWLIST` | *(unset)* | Comma-separated IPs, CIDRs, hostnames. Unset = unrestricted guest egress. Set = DNS + `api.anthropic.com` + these, everything else DROP. |

## Runtime

| Variable | Default | Meaning |
|---|---|---|
| `ANTHROPIC_API_KEY` | *(falls back to OAuth)* | If set, injected into guest as env; no OAuth credential file is copied. |

## Precedence

1. CLI flags (where applicable — `--addr`, `--auth-token`, `--port`).
2. Environment variables.
3. Compiled-in defaults.

## Validation

On startup, config is validated:

- If `ORCHESTRATOR_ADDR` is non-loopback and `ORCHESTRATOR_AUTH_TOKEN` is
  empty, a token is generated and printed to stderr.
- Invalid paths don't fail at startup; they fail on first use with a clear
  error.
- `ORCHESTRATOR_WEBHOOK_URL` is parsed at startup; invalid URLs are logged
  and the sender is disabled (no event delivery, no crash).

## Environment variables the agent binary reads

The guest agent uses two env vars, both set by the orchestrator during
context injection:

- `CLAUDE_DANGEROUSLY_SKIP_PERMISSIONS` — forwarded to Claude Code.
- `ANTHROPIC_API_KEY` — forwarded to Claude Code (if `ANTHROPIC_API_KEY`
  was set on the host).
