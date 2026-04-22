# Security model

Orchestrator gives AI agents root inside disposable MicroVMs. That's
powerful, and it deserves a sober threat model. The top-level
[SECURITY.md](../security.md) is the canonical statement — this page is the
detailed expansion for operators.

## Trust boundaries

Orchestrator is built around three distinct trust zones.

```
┌────────────────────────────────────────────────────────────┐
│ zone 1: operator (you) — has root on the host              │
│   ├── runs the orchestrator process                        │
│   ├── holds the bearer token, webhook secret, etc.         │
│   └── can read every VM's rootfs at will                   │
│                                                            │
│ ┌────────────────────────────────────────────────────────┐ │
│ │ zone 2: orchestrator process — runs as root            │ │
│ │   ├── spawns firecracker (jailed, seccomp'd)           │ │
│ │   ├── programs iptables                                │ │
│ │   └── holds per-VM credentials in memory / guest fs    │ │
│ │                                                        │ │
│ │ ┌────────────────────────────────────────────────────┐ │ │
│ │ │ zone 3: guest VM (KVM-isolated)                    │ │ │
│ │ │   ├── agent code runs with --dangerously-skip-perms│ │ │
│ │ │   ├── has full root inside the VM                  │ │ │
│ │ │   └── can reach the network (NAT'd, optional allow)│ │ │
│ │ └────────────────────────────────────────────────────┘ │ │
│ └────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────┘
```

The main security property is: **zone 3 cannot escape to zone 2 by design.**
Firecracker's KVM + seccomp + jailer chroot is the wall. If that wall is
breached, the attacker owns the host — same as any VM escape.

Orchestrator does **not** protect zone 2 from zone 1 (operators can already
do anything), and does **not** protect the orchestrator itself from a
malicious operator.

## What we protect against

1. **Guest → host escape.** Firecracker's threat model (KVM hypervisor,
   seccomp filter, minimal device surface, jailer chroot, cgroup v2) is our
   primary defence. We inherit it in full.
2. **Guest → guest leakage.** Each VM gets a fresh rootfs copy, its own /24
   subnet, its own TAP device, its own vsock CID, and its own jailer chroot.
   No filesystem, memory, or network state is shared between guests.
3. **Unauthenticated access to a LAN-exposed control plane.** REST and MCP
   servers default to loopback. Non-loopback binds require a bearer token.
4. **Tampering with webhook payloads.** Lifecycle events are HMAC-SHA256
   signed. Receivers verify `X-Orchestrator-Signature`.
5. **Cross-origin attacks against a loopback dashboard.** CORS is off by
   default (same-origin only). WebSocket upgrades enforce the same origin
   policy. Auth tokens can be passed as `Authorization: Bearer` (preferred)
   or `?token=` (for WebSocket upgrades browsers won't let us send headers on).
6. **Shell injection from user-supplied env vars.** Env-var keys are
   validated against `^[A-Za-z_][A-Za-z0-9_]*$`; values are POSIX-single-quoted
   before being written into `/etc/profile.d/claude.sh`.
7. **Leaking the operator's Anthropic credentials.** Credentials are injected
   per-VM, destroyed with the VM, and never logged. Using
   `ANTHROPIC_API_KEY` instead of OAuth avoids copying the refresh token
   into guests entirely.
8. **Webhook SSRF via scheme confusion.** Webhook URL scheme is validated at
   sender construction — only `http` and `https` are accepted.

## What we do NOT protect against

- **A malicious operator.** Anyone with shell access to the orchestrator host
  can read everything Orchestrator reads (including the operator's Claude
  credentials). This is a single-tenant tool.
- **Hostile guest code exfiltrating data.** Agents running inside a VM have
  full outbound NAT by default. Lock down egress with
  `ORCHESTRATOR_EGRESS_ALLOWLIST` if you run prompts that might exfiltrate
  credentials they were given.
- **Claude's own judgement.** `--dangerously-skip-permissions` means the
  agent won't prompt before `rm -rf /`. That's fine inside the VM (it'll be
  destroyed), but Claude may also call the Anthropic API in ways that cost
  money — set `--max-turns` and timeouts.
- **Denial of service via VM sprawl.** Without `ORCHESTRATOR_MAX_CONCURRENT_VMS`
  or `ORCHESTRATOR_TASK_RATE_LIMIT`, a caller with valid credentials can
  exhaust host RAM. Configure limits if you have multiple callers.
- **Supply-chain compromise** of the base rootfs or Firecracker binary. We
  pin versions and document checksums but don't reproduce from sources. Build
  your own image if you need that guarantee.

## Attack paths we've specifically considered

| Attack | Mitigation | Residual risk |
|---|---|---|
| CSRF from a malicious page to a loopback orchestrator | Same-origin CORS by default; WebSocket requires same origin | None while `ORCHESTRATOR_CORS_ORIGINS` is unset; operators who set it are on the hook. |
| XSS in the dashboard → credential theft | Dashboard is React; no `dangerouslySetInnerHTML`; auth token is in `localStorage` behind same-origin | Operators must keep the embedded frontend up to date. |
| Path traversal on task file download | `filepath.Base(filename)` + stat check in results dir (flat; no symlinks created by orchestrator) | An attacker who can plant symlinks in `ResultsDir` already has host root. |
| Timing oracle on token comparison | `subtle.ConstantTimeCompare` | None. |
| Token in URL logged by proxies | `Authorization` header preferred; `?token=` only used by WebSocket upgrade | Operators on hostile LANs should terminate TLS at the orchestrator host. |
| Webhook loopback exploit (credential exfil) | Scheme allowlist (`http`/`https`) + operator sets the URL | Operator must not point webhook at `169.254.169.254/latest/meta-data/`. |
| Concurrent `vm create` race leaking TAPs | Sentinel-under-lock in `VMManager.Create` + deferred rollback | None. |
| Prompt injection into guest env | POSIX single-quoting + POSIX-identifier key validation | Guest is disposable and operator-controlled; residual impact bounded. |

## Hardening checklist

See also [Guides → Deployment](../guides/deployment.md) for a full recipe.

- [ ] Bind the API and MCP servers to loopback (`127.0.0.1`) or to a VPN
      interface — never the public internet.
- [ ] Set `ORCHESTRATOR_AUTH_TOKEN` to a long random value
      (`openssl rand -hex 32`).
- [ ] Use `ANTHROPIC_API_KEY` with organisation-level rate limits set sensibly.
- [ ] Set `ORCHESTRATOR_AUDIT_LOG=/var/log/orchestrator.jsonl` and ship it
      to your SIEM.
- [ ] Set UFW / nftables to DROP FORWARD by default. Orchestrator inserts
      specific ACCEPT rules per VM at position 1.
- [ ] Run the host on a dedicated box. Do not co-locate with human-shell
      workloads.
- [ ] Cap `timeout` and `max_turns` on every task.
- [ ] Monitor `orchestrator_vms_running` and alert if it climbs unbounded.
- [ ] Set `ORCHESTRATOR_EGRESS_ALLOWLIST` to the smallest set of hosts your
      agents need.

## Responsible disclosure

See [SECURITY.md](../security.md). Use GitHub Security Advisories; don't open
a public issue.
