# Changelog

All notable changes to Orchestrator are documented here. This project follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- Pluggable agent runtime registry (`internal/runtime`). Ships `claude` (default) and `shell` adapters; new runtimes register with ~50 lines of code.
- Prometheus-format `/metrics` endpoint (VM counts, task duration histograms, stream bytes).
- Webhook emitter for task lifecycle events, HMAC-SHA256 signed.
- JSON-lines audit log at `ORCHESTRATOR_AUDIT_LOG`.
- Bearer-token authentication on API/MCP servers; auto-generated on non-loopback binds.
- `ANTHROPIC_API_KEY` auth path as an alternative to OAuth credential injection.
- `scripts/install-firecracker.sh` — one-shot Firecracker + jailer + kernel install.
- `scripts/build-rootfs.sh` — reproducible Debian Bookworm rootfs with Node/Chromium/Claude Code pre-baked.
- Centralised runtime config (`internal/config`) driven by `ORCHESTRATOR_*` env vars.
- Apache License 2.0 + NOTICE with third-party attribution.
- CONTRIBUTING, SECURITY, and this changelog.

### Changed
- Default bind addresses for the REST and MCP servers are now `127.0.0.1:8080` / `127.0.0.1:8081` (previously `0.0.0.0`).
- Product renamed back to **Orchestrator**. Go module path remains `github.com/jonnonz1/orchestrator`.
- Task model carries a `runtime` field; defaults to `claude`.
- Rootfs layout + jailer paths are no longer compile-time constants — all configurable via `ORCHESTRATOR_*` env vars.
- Hardcoded `/home/jonno` path removed from the MCP wrapper script; `192.168.50.44` removed from docs.

### Fixed
- (n/a — first public release)

### Security
- MCP server no longer binds `0.0.0.0` by default. When binding non-loopback, a bearer token is mandatory (generated + printed if not provided). `--insecure` flag allows opting out with a loud warning.
