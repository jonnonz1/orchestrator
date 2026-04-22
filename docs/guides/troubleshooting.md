# Troubleshooting

Symptom-keyed. For install-time issues see
[Installation → Troubleshooting install](../getting-started/installation.md#troubleshooting-install).

## VM boot failures

### `jailer failed: …`

The jailer wrote its full log to `/opt/firecracker/vms/<name>/jailer.log`.
Usually one of:

- **`Permission denied`** on `/srv/jailer/firecracker/`. Fix:
  `sudo chown -R root:root /srv/jailer/firecracker`
- **`No such file or directory: /opt/firecracker/firecracker`**. Firecracker
  binary is missing. Re-run `sudo make install-firecracker`.
- **`Error creating cgroup`** — needs cgroup v2. Check
  `stat -fc %T /sys/fs/cgroup` returns `cgroup2fs`.

### `firecracker process not found for jail <name> within 5s`

Jailer forked but the firecracker process didn't come up within 5 seconds.

- Check `/opt/firecracker/vms/<name>/jailer.log` — usually a KVM/kernel
  incompatibility.
- Check `dmesg | tail` — sometimes KVM refuses a VM that asks for features
  the host can't provide.
- Rare: host is too loaded; retry.

## Guest agent not responding

### `agent not ready: agent did not respond within 30s`

The VM booted but the vsock agent never answered.

- Check the VM is actually running: `ps aux | grep firecracker`.
- Check the agent is enabled in the rootfs: on a debug VM, `systemctl status orchestrator-agent`.
- Rebuild the rootfs: `sudo make rootfs`.

If you've modified the agent source, make sure you:
1. Rebuilt the agent binary: `make build-agent`
2. Copied it into the rootfs (see the README's build instructions for the
   mount/copy dance).

## Task failures

### Task status `failed`, no output

- Check Claude creds: `ls -la ~/.claude/.credentials.json`. Must be readable
  by the orchestrator process (or whoever it sudo'd from).
- Check the guest has working DNS: `vm exec --name task-<id> --command "getent hosts api.anthropic.com"`.

### Task status `failed`, `exit_code: 1`

The Claude run itself failed. Use `--no-destroy` to keep the VM and inspect:

```bash
sudo ./bin/orchestrator vm exec \
  --name task-<id> \
  --command "cat /root/task/task.json"
```

### `cost_usd` is 0 on a completed Claude task

The runtime is `claude` but Claude's stream-json output didn't emit a final
`result` event. Usually means Claude hit `--max-turns` or timed out. Check
task logs for a `[exit code: N]` line.

### `unauthorized` from REST

- `ORCHESTRATOR_AUTH_TOKEN` on the server doesn't match the `Authorization`
  header. Compare with `curl -v`.
- Server is bound loopback but you set a token via `--auth-token`. That
  *enables* auth on loopback, not disables. Either stop setting the token or
  actually send it on requests.

### `permission denied` creating VM from REST / MCP

The orchestrator process isn't root. `--auth-token` only guards the HTTP
layer; actually creating a VM still needs root on the host, because
iptables and `/dev/kvm` need it. Run with `sudo`.

## Networking

### VM has no internet

1. `vm exec --name task-<id> --command "ip addr"` — is the guest IP set?
   If not, the systemd-networkd config didn't apply. Rebuild rootfs.
2. `vm exec --name task-<id> --command "getent hosts example.com"` — DNS?
   If not, check `/etc/resolv.conf` in the guest. Orchestrator writes
   8.8.8.8/8.8.4.4.
3. `vm exec --name task-<id> --command "ping -c1 8.8.8.8"` — raw v4? If
   not, check iptables: `sudo iptables -L FORWARD -n -v`. The per-VM ACCEPT
   rules should have non-zero packet counts.
4. `ORCHESTRATOR_EGRESS_ALLOWLIST` set? Maybe the destination isn't listed.
5. Host UFW default FORWARD is DROP with orchestrator's rules appended not
   inserted? Rebuild — orchestrator always inserts at position 1 now.

### `iptables: Permission denied`

Not root. See above.

## Dashboard

### Dashboard shows "unauthorized — set the bearer token"

Non-loopback bind with auth on. Paste the token:

```javascript
localStorage.orchestratorToken = "your-token-here";
location.reload();
```

Or visit the dashboard URL with `#token=YOUR_TOKEN` appended.

### WebSocket disconnects immediately

- Reverse proxy not forwarding upgrade headers. Check the proxy config for
  `Upgrade` + `Connection: upgrade`.
- Auth token required but WS URL doesn't have `?token=…`. The `useWebSocket`
  hook appends this automatically; if you're connecting by hand you need
  to do it too.

## Logs

### `too many open files`

`LimitNOFILE=65536` in the systemd unit. Otherwise:

```bash
sudo prlimit --pid $(pgrep orchestrator) --nofile=65536:65536
```

### `context deadline exceeded` during task

Task hit its `--timeout`. The VM is destroyed in a fresh 30 s context — if
you're also seeing orphaned TAPs or iptables rules, that destroy timed out
too. Filter logs for `destroying task VM` to find the entry.

## When all else fails

- Run with `--no-destroy` + `ORCHESTRATOR_LOG_FORMAT=json` and read the
  sequence of events carefully. Most failures look obvious once you have
  the log.
- File a bug with the bug template; include `orchestrator version`, `uname -a`,
  and the jailer log.
