# Contributing to Orchestrator

Thanks for your interest in improving Orchestrator! This project is in alpha — we'd rather land a messy fix than ship nothing, so don't sweat perfection.

## Project layout

```
cmd/
  orchestrator/      Host binary (CLI + REST + MCP + embedded dashboard)
  agent/             Guest agent (static linux/amd64, vsock:9001)
  test-vsock/        Vsock connectivity smoke test
internal/
  vm/                VM lifecycle (jailer, firecracker process, state recovery)
  network/           TAP devices + iptables NAT
  inject/            Rootfs mutation (network config injection)
  agent/             Shared vsock protocol types
  vsock/             Host-side vsock client
  task/              Task runner + store
  runtime/           Pluggable agent runtimes (claude, shell, …)
  api/               REST API + WebSocket streaming
  mcp/               MCP server (stdio + streamable HTTP)
  stream/            Log fan-out hub
  authn/             Bearer-token middleware
  metrics/           Prometheus text-format collector
  events/            Webhook + audit log sinks
  config/            Centralised env-var driven config
web/                 React/TypeScript dashboard
scripts/             Rootfs + Firecracker install
docs/                Deep-dives, API reference, MCP guide
examples/            Sample task prompts
sdk/                 Python + TypeScript clients
```

## Building

```bash
# One-shot: host + guest agent + embedded frontend
make all

# Piece by piece
make build-frontend     # npm install && vite build
make build              # go build, embeds the frontend
make build-agent        # static linux/amd64 guest binary

# Run tests (no KVM required for unit tests)
make test
make test-race
```

## Publishing a new repo

This repo currently lives under `github.com/jonnonz1/orchestrator`. Before the first public push:

1. `git init` in the project root (this repo does not ship a `.git`).
2. Decide the final GitHub org/name, e.g. `jonnonz1/orchestrator`.
3. `go mod edit -module github.com/<owner>/orchestrator`
4. Update import paths: `grep -rl jonnonz1/orchestrator | xargs sed -i 's|jonnonz1/orchestrator|<owner>/orchestrator|g'`
5. `go mod tidy && make test`
6. Push.

We keep the module path unchanged inside this repo so that local contributors don't need to do the rename.

## Pre-flight before submitting a PR

```bash
make fmt vet test
```

CI runs the same checks plus `staticcheck` and the frontend build.

## Style

- **Go**: gofmt + go vet enforced. Error messages lowercased, no trailing punctuation.
- **Comments**: explain *why* the non-obvious bits exist. Don't narrate what obvious code does.
- **Tests**: unit-testable code lives next to its subject in `*_test.go`. Integration tests that need a VM are in `cmd/test-vsock/` or gated on `ORCHESTRATOR_INTEGRATION=1`.
- **Commits**: imperative mood, under 72 chars. Reference the issue/PR number if there is one.

## What's a good first issue?

Anything tagged [`good-first-issue`](https://github.com/<owner>/orchestrator/labels/good-first-issue). As of alpha:

- Add a new agent runtime adapter (gemini-cli, aider, codex)
- Harden the rootfs build (reproducibility, smaller footprint)
- Write an example prompt that exercises a fun use case
- Improve a CLI error message where Orchestrator currently says "firecracker failed"
- Cross-distro testing (Debian, Fedora, Arch)

## Reporting bugs

Use the bug issue template. Include:

- Host distro + kernel version
- Output of `orchestrator version`
- `/tmp/jailer.log` from the failing VM (or `journalctl -u orchestrator`)
- The prompt or API request that triggered it

## Security bugs

**Don't open a public issue.** See the [SECURITY policy](https://github.com/jonnonz1/orchestrator/blob/main/SECURITY.md) for the disclosure process.

## Community

- GitHub Discussions — for design questions and RFCs
- GitHub Issues — for bug reports and feature requests

## License

By contributing you agree that your contributions will be licensed under the Apache License 2.0.
