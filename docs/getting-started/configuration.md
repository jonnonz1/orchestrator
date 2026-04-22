# Configuration

Every tunable is an environment variable or a CLI flag — no config file to
edit. CLI flags win over env vars; env vars win over compiled-in defaults.

See [reference/configuration](../reference/configuration.md) for the full
list; this page covers the ones you'll actually touch.

## Paths

All four of these live under `ORCHESTRATOR_FC_BASE` (default `/opt/firecracker`).
Override them if you want Firecracker + its state on a different disk.

| Variable | Default | Purpose |
|---|---|---|
| `ORCHESTRATOR_FC_BASE` | `/opt/firecracker` | Root of the Firecracker layout. |
| `ORCHESTRATOR_JAILER_BASE` | `/srv/jailer/firecracker` | Per-VM jailer chroot root. |
| `ORCHESTRATOR_RESULTS_DIR` | `$FC_BASE/results` | Where task output files land on the host. |
| `ORCHESTRATOR_VM_DIR` | `$FC_BASE/vms` | Per-VM state directories. |

## Bind addresses

| Variable | Default | Purpose |
|---|---|---|
| `ORCHESTRATOR_ADDR` | `127.0.0.1:8080` | REST API + dashboard. |
| `ORCHESTRATOR_MCP_ADDR` | `127.0.0.1:8081` | MCP server (Streamable HTTP). |

**Default is loopback.** Non-loopback binds require `ORCHESTRATOR_AUTH_TOKEN`
(or `--auth-token`). A token is auto-generated and printed if you forget.

## Authentication

| Variable | Default | Purpose |
|---|---|---|
| `ORCHESTRATOR_AUTH_TOKEN` | *(none)* | Bearer token required on non-loopback servers. Use `openssl rand -hex 32`. |
| `ORCHESTRATOR_CORS_ORIGINS` | *(none)* | Comma-separated list of origins allowed to make cross-origin requests. Empty = same-origin only. |
| `ANTHROPIC_API_KEY` | *(falls back to OAuth)* | If set, injected into the guest as env instead of copying the host's OAuth refresh token. |

## Observability

| Variable | Default | Purpose |
|---|---|---|
| `ORCHESTRATOR_AUDIT_LOG` | *(disabled)* | Path to a JSON-lines audit log. Append-only. |
| `ORCHESTRATOR_WEBHOOK_URL` | *(disabled)* | HTTP(S) URL that receives task lifecycle events. |
| `ORCHESTRATOR_WEBHOOK_SECRET` | *(none)* | HMAC-SHA256 secret for signing webhook bodies. |
| `ORCHESTRATOR_LOG_FORMAT` | `text` | Set to `json` for structured slog output. |

## Guardrails

| Variable | Default | Purpose |
|---|---|---|
| `ORCHESTRATOR_MAX_CONCURRENT_VMS` | *(unlimited)* | Semaphore on running VMs. Reject further task creations beyond this cap. |
| `ORCHESTRATOR_TASK_RATE_LIMIT` | *(unlimited)* | Token-bucket tasks-per-minute. HTTP 429 when exceeded. |
| `ORCHESTRATOR_EGRESS_ALLOWLIST` | *(unrestricted)* | Comma-separated IPs/CIDRs/hostnames. VM outbound is restricted to this set + DNS + `api.anthropic.com`. |

## A useful environment file

For a host that exposes the server on a LAN with audit logging and concurrency
caps:

```bash
# /etc/default/orchestrator
ORCHESTRATOR_ADDR=0.0.0.0:8080
ORCHESTRATOR_MCP_ADDR=0.0.0.0:8081
ORCHESTRATOR_AUTH_TOKEN=replace-with-openssl-rand-hex-32
ORCHESTRATOR_CORS_ORIGINS=https://orchestrator.internal
ORCHESTRATOR_AUDIT_LOG=/var/log/orchestrator.jsonl
ORCHESTRATOR_WEBHOOK_URL=https://hooks.internal/orchestrator
ORCHESTRATOR_WEBHOOK_SECRET=another-openssl-rand-hex-32
ORCHESTRATOR_MAX_CONCURRENT_VMS=8
ORCHESTRATOR_TASK_RATE_LIMIT=30
ORCHESTRATOR_LOG_FORMAT=json
```

Load with `systemd`:

```ini
# /etc/systemd/system/orchestrator.service
[Service]
EnvironmentFile=/etc/default/orchestrator
ExecStart=/usr/local/bin/orchestrator serve
```

Full service unit recipe in [Operating the server](../guides/operating-server.md).
