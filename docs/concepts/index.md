# Concepts

How Orchestrator is put together and why.

- **[Architecture](architecture.md)** — the host process, the guest agent,
  and the five things that move between them.
- **[Security model](security-model.md)** — what Orchestrator protects
  against, what it explicitly doesn't, and the trust boundaries.
- **[Runtimes](runtimes.md)** — the pluggable abstraction that lets you run
  Claude, shell, or your own binary inside a VM.
- **[Networking & vsock](networking.md)** — TAP devices, iptables NAT, the
  egress allowlist, and why the host ↔ guest channel is vsock not SSH.
- **[Snapshots & warm pool](snapshots.md)** — how `snapshot create`/`restore`
  work and how to pre-warm VMs for sub-second task starts.
