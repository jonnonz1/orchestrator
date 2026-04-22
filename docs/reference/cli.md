# CLI reference

Orchestrator is a single binary with subcommands. Every subcommand that
touches VMs needs root — run with `sudo`.

```
orchestrator <command> [flags]
```

## Subcommands

| Command | Requires root | What it does |
|---|---|---|
| `vm create` | yes | Create and boot a new VM. |
| `vm list` | no | List known VMs. |
| `vm get` | no | Print a VM's metadata as JSON. |
| `vm stop` | yes | Kill the firecracker process; keep state on disk. |
| `vm destroy` | yes | Stop + remove chroot + remove iptables + remove state. |
| `task run` | yes | Run a task end-to-end. |
| `serve` | yes | Start the REST API + embedded dashboard. |
| `mcp` | — | MCP server over stdio. |
| `mcp-serve` | yes (if it creates VMs) | MCP server over Streamable-HTTP. |
| `snapshot create\|restore\|list\|delete` | yes | Manage VM snapshots. |
| `version` | no | Print version and exit. |
| `help` | no | Show usage. |

## `vm create`

```bash
sudo ./bin/orchestrator vm create \
  --name my-vm \
  --ram 2048 \
  --vcpus 2
```

| Flag | Default | Notes |
|---|---|---|
| `--name` | *(required)* | Alphanumeric + hyphens; cannot start with hyphen. |
| `--ram` | 512 | MB; 128–32768. |
| `--vcpus` | 1 | 1–32. |

Prints the created VM as JSON on success. Errors exit non-zero.

## `vm list`

```bash
./bin/orchestrator vm list
```

Tabular output: `NAME STATE RAM VCPUS GUEST-IP PID`.

## `vm get`

```bash
./bin/orchestrator vm get my-vm
# or
./bin/orchestrator vm get --name my-vm
```

JSON dump of the VMInstance struct.

## `vm stop` / `vm destroy`

```bash
sudo ./bin/orchestrator vm stop my-vm
sudo ./bin/orchestrator vm destroy my-vm
```

`stop` keeps rootfs/metadata on disk (you can't resume yet — cold `vm start`
isn't a thing, but snapshot/restore is). `destroy` wipes everything.

## `task run`

```bash
sudo ./bin/orchestrator task run \
  --prompt "Take a screenshot of example.com" \
  --runtime claude \
  --ram 4096 \
  --timeout 300
```

Blocking. Output streams to your terminal. See
[Guides → Running a task](../guides/running-tasks.md).

## `serve`

```bash
sudo ./bin/orchestrator serve
# or
sudo ./bin/orchestrator serve --addr 0.0.0.0:8080 --auth-token $TOKEN
```

| Flag | Default | Notes |
|---|---|---|
| `--addr` | `127.0.0.1:8080` | Bind address. |
| `--port` | 8080 | Shorthand for `--addr 127.0.0.1:<port>`. |
| `--auth-token` | *(from env or generated on non-loopback)* | Bearer token. |
| `--insecure` | off | Opt out of auth on non-loopback (dangerous). |

## `mcp` (stdio)

```bash
sudo ./bin/orchestrator mcp
```

No flags. Reads JSON-RPC from stdin, writes to stdout. Wire it into Claude
Code via the local MCP config:

```json
{"mcpServers":{"orchestrator":{"command":"sudo","args":["/path/to/bin/orchestrator","mcp"]}}}
```

## `mcp-serve` (HTTP)

```bash
sudo ./bin/orchestrator mcp-serve --addr 0.0.0.0:8081
```

Same flags as `serve`. Clients connect to `http://host:8081/mcp` with
Streamable-HTTP framing.

## `snapshot create`

```bash
sudo ./bin/orchestrator snapshot create \
  --vm my-vm \
  --name my-vm-warm \
  --resume        # keep the source VM running after snapshot
```

## `snapshot restore`

```bash
sudo ./bin/orchestrator snapshot restore \
  --name my-vm-warm \
  --vm task-42
```

## `snapshot list` / `snapshot delete`

```bash
./bin/orchestrator snapshot list
sudo ./bin/orchestrator snapshot delete --name my-vm-warm
```

## `version`

```bash
./bin/orchestrator version
# orchestrator <version>
```

Aliases: `-v`, `--version`.

## `help`

```bash
./bin/orchestrator help
```

Aliases: `-h`, `--help`.

## Environment variables the CLI reads

All the `ORCHESTRATOR_*` variables. See
[Reference → Configuration](configuration.md) for the full list. The most
common:

- `ORCHESTRATOR_FC_BASE` — where Firecracker + VMs live.
- `ORCHESTRATOR_ADDR` — default `--addr` for `serve`.
- `ORCHESTRATOR_AUTH_TOKEN` — default `--auth-token`.
- `ANTHROPIC_API_KEY` — used instead of OAuth credentials when set.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success. |
| 1 | Error: invalid flags, missing required value, operation failed. |
| (non-zero, from task) | `task run` surfaces the agent's exit code when the task completed but the agent itself returned non-zero. |
