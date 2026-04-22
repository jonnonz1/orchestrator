# Security Policy

Orchestrator gives AI agents root inside disposable MicroVMs. That's powerful, and it deserves a sober threat model. This document covers what we guard against, what we deliberately don't, and how to report a security bug.

## Reporting a vulnerability

**Do not open a public issue.** Use [GitHub Security Advisories](https://github.com/jonnonz1/orchestrator/security/advisories/new) to report privately. Include:

- A description of the issue
- Steps to reproduce (ideally a minimal repro)
- Your assessment of impact
- Whether you want public credit in the advisory

We aim to acknowledge within 72 hours. Fixes land in a private branch, then we publish a GitHub Security Advisory and coordinated CVE if warranted.

## Threat model

Orchestrator's security story is shaped by one truth: **the host process runs as root and executes code on behalf of autonomous agents that the operator trusts.** We protect *the guest from the host's other guests* and *the network from the guest* — we do not try to protect the host from the operator or from the agents the operator runs.

### What we protect against

1. **Guest → host escape.** Firecracker's threat model (KVM hypervisor, seccomp filter, minimal device surface, jailer chroot + cgroup v2) is our primary defence. We inherit it in full — we do not poke holes in the jailer's default allowlist.
2. **Guest → guest leakage.** Each VM gets a fresh rootfs copy, its own /24 subnet, its own TAP device, its own vsock CID, and its own jailer chroot. No filesystem, memory, or network state is shared between guests.
3. **Unauthenticated access to a LAN-exposed control plane.** The REST and MCP servers default to loopback. When an operator explicitly binds a non-loopback address, a bearer token is required; Orchestrator refuses to start without one unless `--insecure` is passed.
4. **Tampering with webhook payloads.** Lifecycle events are HMAC-SHA256 signed with `ORCHESTRATOR_WEBHOOK_SECRET`. Receivers should verify the `X-Orchestrator-Signature` header.
5. **Leaking the operator's Anthropic credentials.** Credentials are injected per-VM, destroyed with the VM, and never logged. Using `ANTHROPIC_API_KEY` instead of OAuth avoids copying the refresh token into guests entirely.

### What we do NOT protect against

- **A malicious operator.** Anyone with shell access to the Orchestrator host can read everything Orchestrator reads (including the operator's Claude credentials). This is a single-tenant tool.
- **Hostile guest code exfiltrating data.** Agents running inside a VM have full outbound NAT by default. If you run prompts that might try to exfiltrate the credentials injected into the same VM, lock down egress (coming: `--egress-allowlist`) or use short-lived API keys.
- **The Claude agent's own judgement.** `--dangerously-skip-permissions` means the agent won't prompt for confirmation before `rm -rf /`. That's fine inside the VM (it'll be destroyed anyway), but Claude may also call the Anthropic API in ways that cost money — set `--max-turns` and timeout caps.
- **Denial of service via VM sprawl.** A caller with valid credentials can spawn VMs until the host runs out of RAM. Rate limits + concurrency caps are on the roadmap.
- **Supply-chain compromise** of the base rootfs or Firecracker binary. We pin versions and document the expected checksums, but we do not re-derive a reproducible rootfs from sources. Build your own image if you need that guarantee.

## Hardening checklist for production deployments

- [ ] Bind the API and MCP servers to loopback (`127.0.0.1`) or to a VPN interface — never the public internet.
- [ ] Set `ORCHESTRATOR_AUTH_TOKEN` to a long, random value (`openssl rand -hex 32`).
- [ ] Use `ANTHROPIC_API_KEY` with Anthropic's organisation-level rate limits set sensibly.
- [ ] Set `ORCHESTRATOR_AUDIT_LOG=/var/log/orchestrator.jsonl` and ship it to your SIEM.
- [ ] Set UFW / nftables to DROP FORWARD by default; Orchestrator inserts specific ACCEPT rules per VM at position 1.
- [ ] Run the host on a dedicated box. Do not co-locate with human-shell workloads.
- [ ] Cap `timeout` and `max_turns` on every task.
- [ ] Monitor the `orchestrator_vms_running` metric and alert if it climbs unbounded.

## Known risks tracked in issues

- Full outbound egress is the default — egress policy support is designed but not shipped.
- No per-caller rate limiting.
- No snapshot signing — warm-pool images aren't verified.
- Frontend served without CSRF protection when auth is disabled (loopback only).

## Our commitment

- We will never add telemetry that phones home by default.
- We will not ship features that require the operator to send prompts or source code to a third-party service Orchestrator controls.
- When we change the security posture (new auth default, new protocol, new trust boundary), we will tag the release `SECURITY` in the changelog.
