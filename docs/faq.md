# FAQ

## Why Firecracker instead of Docker?

Docker is namespaces + cgroups inside the host kernel. A kernel bug or
misconfigured capability can become a host compromise. Firecracker runs each
guest in its own KVM VM with its own kernel; the guest-to-host surface is
just the KVM ioctl interface, which has been scrutinised by cloud providers
for a decade.

For agentic workloads where the agent can install packages, run servers,
download untrusted code, the container boundary isn't tight enough. KVM is.

## Why not gVisor?

gVisor is a good middle-ground (namespace-shaped isolation with a Go-based
user-mode kernel). It's faster to start than Firecracker, but performance
under heavy syscall load is poor (each syscall routes through gVisor's
Sentry). Firecracker wins on both isolation and steady-state performance.

We might add a gVisor backend one day as a cheaper option for trusted
workloads. Not a priority.

## Why vsock instead of SSH?

Speed to agent-ready. vsock is available milliseconds after the kernel
boots — before SSH keys are generated, before networking is configured.
That's half our 4-second boot budget saved.

Plus: no SSH daemon in the guest means one fewer attack surface. The agent
binary is the only listener.

## Why Go?

One binary, no runtime, static linking, fast enough, low memory overhead,
good stdlib for raw syscalls. Nothing fancier needed.

## Can I run this on macOS?

No — Firecracker is Linux-only. You can run the orchestrator binary on a
Linux VM on a Mac (UTM, Colima, Lima), but you're paying a lot of overhead
for the virtualisation of the virtualiser.

## Can I run this on Windows?

See above. Run in WSL2, but nested virtualisation support is spotty.

## Can I run this on ARM?

Untested but likely works. Firecracker ships arm64. You'd need to rebuild
the rootfs for arm64 and tweak `scripts/install-firecracker.sh`. PR welcome.

## How much does a Claude task cost?

Depends on the prompt. Simple screenshots: $0.01–$0.05. Codebase analysis:
$0.10–$0.50. Anything multi-turn with tool use: $0.50–$5. Orchestrator
surfaces this as `cost_usd` in the task result. Set `--max-turns` and
`--timeout` if you're worried about runaway cost.

If cost matters, use `ANTHROPIC_API_KEY` with Anthropic's organisation-level
rate limits to cap exposure.

## Can I run Aider / Codex / gemini-cli instead of Claude?

Yes — runtimes are pluggable. See
[Reference → Runtime plugin API](reference/runtime-plugin.md). Currently
we ship `claude` and `shell`; adding a new runtime is ~50 lines.

## What happens if the host crashes during a task?

- The VM and its task are lost.
- iptables rules, TAP devices, chroots **may** leak if the crash happened
  mid-setup — orchestrator cleans these up idempotently at the next start
  via `recoverState`, but rules installed but not yet associated with a
  tracked VM are orphaned.
- Audit log entries that had been flushed are persistent; entries buffered
  at crash time are lost.

Treat orchestrator like a best-effort batch runner, not a durable queue.

## Can I use this in a multi-tenant setup?

Not well. The task store is in-memory, concurrency is global, there's no
per-caller rate limiting. For multi-tenant, put a gateway in front that
keys on caller identity and runs its own limits.

A durable queue + multi-host coordination is out of scope for v1.

## Why is the default VM 2 GB?

Chromium + npm install + a running Claude agent eats memory. 2 GB is the
smallest we've seen reliably work for the typical "take a screenshot,
build a React app, run tests" shape. Drop to 512 MB for shell-only tasks.

## Why is my task so slow?

1. First run: the rootfs cold-cache. Re-running the same task should be
   much faster.
2. Claude can take 20-30 s of its own thinking before any tool use. That's
   on the model, not orchestrator.
3. Check `vm_boot_seconds` in Prometheus — if that's well above 4 s,
   something's wrong with your host (disk I/O? swapping?).

## How do I report a bug / contribute / ask for a feature?

- Bugs: [GitHub Issues](https://github.com/jonnonz1/orchestrator/issues/new/choose),
  pick the "Bug" template.
- Features: same issue tracker, "Feature request" template.
- Security: [GitHub Security Advisory](https://github.com/jonnonz1/orchestrator/security/advisories/new).
  See [SECURITY.md](security.md).

## Can I cite this project?

Sure. BibTeX:

```bibtex
@software{orchestrator,
  title  = {Orchestrator: Self-hosted MicroVM orchestrator for AI agents},
  author = {Jonno Nz},
  year   = {2026},
  url    = {https://github.com/jonnonz1/orchestrator},
  note   = {Apache License 2.0},
}
```
