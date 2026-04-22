# Running a task

Three ways to submit a task: **CLI**, **REST**, **MCP**. All three drive the
same `task.Runner` under the hood, so behaviour is identical.

## CLI

```bash
sudo ./bin/orchestrator task run \
  --prompt "Summarise the README of https://github.com/jonnonz1/orchestrator" \
  --ram 4096 \
  --timeout 300
```

Output streams to your terminal. Exit code is 0 on success, non-zero on
failure (including Claude's own non-zero exit).

All CLI flags:

| Flag | Default | Meaning |
|---|---|---|
| `--prompt`, `-p` | *(required)* | The task prompt. Can be long. |
| `--runtime` | `claude` | `claude` or `shell`; any registered runtime name. |
| `--ram` | 2048 | Guest RAM in MB (128–32768). |
| `--vcpus` | 2 | Guest vCPU count (1–32). |
| `--timeout` | 600 | Wall-clock timeout in seconds. Task fails on expiry. |
| `--max-turns` | *(unlimited)* | For `claude`, caps the number of tool-use turns. |
| `--no-destroy` | off | Leave the VM running when the task completes. Useful for debugging. |

## REST

```bash
curl -X POST http://127.0.0.1:8080/api/v1/tasks \
  -H "Authorization: Bearer $ORCHESTRATOR_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
        "prompt": "…",
        "runtime": "claude",
        "ram_mb": 4096,
        "timeout": 300,
        "max_turns": 20
      }'
```

You get a JSON task object back with a UUID `id`. Poll
`GET /api/v1/tasks/<id>` or open a WebSocket to
`GET /api/v1/tasks/<id>/stream` for live output.

Full REST reference at [Reference → REST API](../reference/rest-api.md).

## MCP

Once the MCP server is running and wired into Claude Code, just ask:

> *"Use orchestrator's run_task to clone torvalds/linux, count commits by
> author, and return a summary table."*

Claude calls `run_task` with the prompt, the task runs end-to-end, and the
final result (including any text result files) comes back to the parent
Claude as the MCP tool call's return value.

Full MCP reference at [Reference → MCP server](../reference/mcp-server.md).

## Injecting env vars

```bash
curl -X POST http://127.0.0.1:8080/api/v1/tasks \
  -H "Authorization: Bearer $ORCHESTRATOR_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
        "prompt": "echo \"git token: $GITHUB_TOKEN\" > /root/output/info.txt",
        "runtime": "shell",
        "env_vars": { "GITHUB_TOKEN": "ghp_…" }
      }'
```

Keys must match `^[A-Za-z_][A-Za-z0-9_]*$`. Values are single-quoted into
`/etc/profile.d/claude.sh`, which means **no shell expansion** — `$FOO` stays
literal, no command substitution, no backslash escapes.

## Injecting files

Pass a `files` map in the REST payload (CLI doesn't currently expose this —
use the REST endpoint):

```json
{
  "prompt": "Refactor /root/workspace/main.go …",
  "files": {
    "/root/workspace/main.go": "package main\n\nfunc main() {}\n",
    "/root/workspace/go.mod":  "module demo\n"
  }
}
```

Files are written before the agent starts, with mode 0644.

## Choosing a timeout

- **Claude tasks:** start at 5 minutes, bump to 15 if the agent needs to
  install things or run long builds.
- **Shell tasks:** the timeout should reflect the deterministic upper bound
  of your script.
- Orchestrator enforces the timeout at the task-runner level by cancelling
  the task context. The VM is destroyed in a fresh 30s context so cleanup
  still happens even if the wall-clock expired.

## Collecting results

Anything written under `/root/output/` or newly created under `/root/` (not
dotfiles, not `/root/.claude/`, not `/root/task/`) is downloaded to
`/opt/firecracker/results/<task-id>/` on the host and listed in the task's
`result_files` field.

Limits:

- Files are read fully into memory then written to disk. Don't produce
  single files larger than ~200 MB. There's no streaming file transfer yet.
- Filenames are flattened (`filepath.Base`), so deeply-nested results lose
  their structure. Name result files distinctly to avoid clobbers — the
  runner renames duplicates to `name-2.ext`, `name-3.ext`, etc.

## Canceling a task

- **CLI:** Ctrl-C; the signal handler cancels the task context, the running
  VM is destroyed.
- **REST:** `DELETE /api/v1/tasks/<id>` — destroys the VM, marks the task
  `cancelled`.
- **MCP:** the MCP call is synchronous; drop the underlying HTTP
  connection and the server will see the cancelled context.

## Debugging a failed task

1. Run with `--no-destroy` and `--timeout 60` so the VM sticks around.
2. `sudo ./bin/orchestrator vm list` to find the VM name.
3. `sudo ./bin/orchestrator vm exec --name task-<id> --command "cat /var/log/syslog"`
   to poke around. (Via REST: `POST /api/v1/vms/<name>/exec`.)
4. Finally `sudo ./bin/orchestrator vm destroy --name task-<id>`.
