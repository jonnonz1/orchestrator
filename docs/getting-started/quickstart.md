# Quickstart

Assumes you've finished [installation](installation.md) and `sudo ./bin/orchestrator
vm list` works.

## Run a one-shot task from the CLI

```bash
sudo ./bin/orchestrator task run \
  --prompt "Take a screenshot of https://example.com and save it to /root/output/example.png" \
  --ram 4096
```

What happens:

1. A fresh VM boots in ~4 seconds.
2. The guest agent starts and reports ready over vsock.
3. Claude credentials, the task prompt, and a settings file are injected.
4. Claude Code runs inside the VM with full tool access, streams output.
5. Files written under `/root/output/` are downloaded to
   `/opt/firecracker/results/<task-id>/` on the host.
6. The VM is destroyed and all trace (TAP, iptables, chroot, state dir) is
   cleaned up.

Result files end up on the host — check `/opt/firecracker/results/<task-id>/`.

## Start the server + dashboard

```bash
sudo ./bin/orchestrator serve          # REST API + web UI on 127.0.0.1:8080
```

Open [http://localhost:8080](http://localhost:8080) in a browser. Use the
dashboard to:

- Spawn tasks with live streaming output.
- Browse existing VMs and their state.
- Preview and download result files.

## Start the MCP server (Claude Code)

```bash
sudo ./bin/orchestrator mcp-serve      # Streamable HTTP on 127.0.0.1:8081
```

Add it to your Claude Code config so Claude can delegate sub-tasks:

```json
{
  "mcpServers": {
    "orchestrator": {
      "type": "http",
      "url": "http://127.0.0.1:8081/mcp"
    }
  }
}
```

Now ask Claude anything that wants a sandbox:

> *"Use orchestrator to clone torvalds/linux, count commits by author, and
> return a summary."*

Claude invokes `run_task`, a fresh VM spins up, a sub-Claude does the work,
files come back, VM shreds. Host Claude only sees the result.

## Switch runtimes

Claude is the default. Run a non-agent shell task instead:

```bash
sudo ./bin/orchestrator task run \
  --prompt "curl -sS https://api.github.com/repos/jonnonz1/orchestrator | jq .stargazers_count > /root/output/stars.txt" \
  --runtime shell \
  --ram 512
```

See [Concepts → Runtimes](../concepts/runtimes.md) for how runtimes are wired
up and how to add your own.

## Expose on a LAN

**By default the servers bind to loopback only.** To share them with other
machines on your LAN:

```bash
sudo ORCHESTRATOR_AUTH_TOKEN=$(openssl rand -hex 32) \
  ./bin/orchestrator serve --addr 0.0.0.0:8080
```

Non-loopback binds **require** an auth token. Orchestrator refuses to start
without one unless you pass `--insecure` (don't). The dashboard supports
three ways to pass the token:

1. Paste it into `localStorage.orchestratorToken` in devtools.
2. Visit the URL with `#token=<token>` — the dashboard extracts it, stashes
   it, and rewrites the URL.
3. Send `Authorization: Bearer <token>` on raw API calls (SDKs do this for
   you).

## Next steps

- [Configuration reference](configuration.md) — every env var, every flag.
- [Running a task in anger](../guides/running-tasks.md) — prompt files, env
  vars, user-supplied files, timeouts, result collection.
- [Operating the server](../guides/operating-server.md) — systemd unit, log
  rotation, restarts, recovery.
- [Observability](../guides/observability.md) — Prometheus metrics, structured
  logs, audit log, webhooks.
