# Operating the server

Production-shaped recipe for running orchestrator as a systemd-managed
service with logs, rotation, and crash recovery.

## systemd unit

```ini
# /etc/systemd/system/orchestrator.service
[Unit]
Description=Orchestrator MicroVM server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
EnvironmentFile=/etc/default/orchestrator
ExecStart=/usr/local/bin/orchestrator serve
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

# Write systemd journal + a plain log file. pick one or both.
StandardOutput=append:/var/log/orchestrator.log
StandardError=append:/var/log/orchestrator.log

[Install]
WantedBy=multi-user.target
```

MCP server as a separate unit:

```ini
# /etc/systemd/system/orchestrator-mcp.service
[Unit]
Description=Orchestrator MCP server
After=orchestrator.service
Requires=orchestrator.service

[Service]
Type=simple
User=root
EnvironmentFile=/etc/default/orchestrator
ExecStart=/usr/local/bin/orchestrator mcp-serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Environment file — see [Configuration](../getting-started/configuration.md):

```bash
# /etc/default/orchestrator
ORCHESTRATOR_ADDR=0.0.0.0:8080
ORCHESTRATOR_MCP_ADDR=0.0.0.0:8081
ORCHESTRATOR_AUTH_TOKEN=replace-me
ORCHESTRATOR_AUDIT_LOG=/var/log/orchestrator.jsonl
ORCHESTRATOR_LOG_FORMAT=json
ORCHESTRATOR_MAX_CONCURRENT_VMS=8
ORCHESTRATOR_TASK_RATE_LIMIT=30
```

Enable:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now orchestrator orchestrator-mcp
sudo systemctl status orchestrator
```

## Log rotation

With `ORCHESTRATOR_LOG_FORMAT=json` and the `StandardOutput=append:` unit, a
plain logrotate config is enough:

```
# /etc/logrotate.d/orchestrator
/var/log/orchestrator.log /var/log/orchestrator.jsonl {
    daily
    rotate 14
    compress
    delaycompress
    copytruncate
    missingok
    notifempty
}
```

`copytruncate` matters because systemd's `append:` keeps an open file
descriptor — a rename would lose logs.

## Restart semantics

Orchestrator keeps per-VM metadata on disk under
`/opt/firecracker/vms/<name>/metadata.json`. On startup, `VMManager.recoverState`
walks that directory and re-registers any VM whose `pid` is still alive.

So a restart is cheap:

- Running VMs survive.
- The in-memory task store is lost — in-flight tasks are effectively
  orphaned (the VM is still there, the host process has no record of it).
  Use the audit log to reconstruct.
- The stream hub is lost — any WebSocket clients reconnect automatically
  (the frontend does this) but only see new events.

To avoid orphaned tasks, drain before restart:

```bash
sudo systemctl stop orchestrator
# wait until no running VMs remain (poll /api/v1/vms or check vm list)
```

`systemctl stop` sends SIGTERM; orchestrator's signal handler unwinds
gracefully — **but task in-flight is not cancelled; only the HTTP server is
shut down**. The tasks themselves keep running in their VMs. This is by
design; the alternative (killing mid-task) loses more data than it saves.

## Scaling up RAM

- Each VM costs its `ram_mb` in host RAM plus ~50 MB of firecracker overhead.
- Set `ORCHESTRATOR_MAX_CONCURRENT_VMS` to a safe fraction of host RAM /
  default VM RAM.
- Monitor `orchestrator_vms_running` via Prometheus; alert at 80% of the
  cap.

## Scaling up disk

- Each VM is a sparse copy of the base rootfs (~4 GB nominal, <200 MB
  actual in practice).
- Task results accumulate in `/opt/firecracker/results/<task-id>/`. Prune:

```bash
find /opt/firecracker/results -mindepth 1 -maxdepth 1 -type d -mtime +7 -exec rm -rf {} +
```

## Upgrading

1. `systemctl stop orchestrator orchestrator-mcp`
2. Replace `/usr/local/bin/orchestrator` with the new binary.
3. `systemctl start orchestrator orchestrator-mcp`
4. Existing VMs are recovered.

Rootfs upgrades (new Claude Code version, new packages) are independent:
rebuild the rootfs, replace `/opt/firecracker/rootfs/base-rootfs.ext4`,
**existing VMs are unaffected** — new VMs will boot from the new template.

## Backups

There's not much persistent state worth backing up:

- `/var/log/orchestrator.jsonl` — audit log, worth keeping.
- `/opt/firecracker/results/` — task outputs. Keep as long as you need.
- `/opt/firecracker/rootfs/base-rootfs.ext4` — reproducible from scratch;
  back up if rebuilding is expensive.
- `/etc/default/orchestrator` — operational config, keep.

Don't back up `/opt/firecracker/vms/` — it's per-VM ephemeral state.
