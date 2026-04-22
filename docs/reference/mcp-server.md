# MCP Server — Network Claude Code Delegation

The orchestrator exposes a Model Context Protocol (MCP) server that allows Claude Code on any machine in your network to delegate tasks to isolated Firecracker MicroVMs.

## How It Works

```
Client (Claude Code)                      Server (orchestrator host)
─────────────────────                     ──────────────────────

Claude Code calls                         MCP Server (:8081)
  run_task(prompt="...")  ──── HTTP ───►    │
                                            ├── Creates a MicroVM (~4s)
                                            ├── Injects Claude credentials
                                            ├── Runs Claude Code inside VM
                                            ├── Claude Code does the work
                                            ├── Downloads result files
                                            ├── Destroys VM
  ◄── task result + file list ────────────  │

Claude Code calls                           │
  get_task_file("code.js")  ── HTTP ─────►  ├── Returns file as text
  ◄── file contents ─────────────────────   │

  get_task_file("screenshot.png") ───────►  ├── Returns image (viewable)
  ◄── base64 image ─────────────────────    │
```

## Setup

### 1. Start the MCP Server

On the server:

```bash
# Default: localhost only, with auto-generated bearer token printed on startup
sudo ./bin/orchestrator mcp-serve

# Expose on LAN (requires --auth-token or explicit --insecure)
sudo ./bin/orchestrator mcp-serve --addr 0.0.0.0:8081 --auth-token "$(openssl rand -hex 32)"
```

Ensure port 8081 is open in the firewall:

```bash
sudo ufw allow 8081/tcp
```

### 2. Configure Claude Code on Client Machines

On any machine that should be able to delegate tasks, add to `~/.claude/mcp.json`:

```json
{
  "mcpServers": {
    "orchestrator": {
      "type": "http",
      "url": "http://<your-server-ip>:8081/mcp",
      "headers": {
        "Authorization": "Bearer <your-auth-token>"
      }
    }
  }
}
```

Replace `<your-server-ip>` and `<your-auth-token>` with your values.

### 3. Use It

From Claude Code on the client machine, just ask naturally:

> "Use the orchestrator to write a Python web scraper that extracts headlines from news.ycombinator.com"

> "Use the orchestrator to take a screenshot of my website at https://example.com"

> "Use the orchestrator to create a React component for a login form, with tests"

Claude Code will automatically use the MCP tools to delegate the work.

## Available Tools

### run_task

The primary tool. Creates an isolated VM, runs Claude Code inside it with your prompt, collects results, and destroys the VM.

**Parameters:**

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `prompt` | string | yes | - | The prompt to give to Claude Code inside the VM |
| `ram_mb` | number | no | 2048 | RAM in MB for the VM |
| `vcpus` | number | no | 2 | Number of vCPUs |
| `timeout` | number | no | 600 | Timeout in seconds |
| `max_turns` | number | no | 50 | Max Claude Code turns |
| `output_dir` | string | no | /root/output | Directory inside VM to collect results from |

**Returns:** Task ID, status, exit code, output text, list of result files, cost, duration.

**Example response:**
```json
{
  "task_id": "a3bfca80",
  "status": "completed",
  "exit_code": 0,
  "result_files": ["index.html", "screenshot.png", "app.js"],
  "cost_usd": 0.2145,
  "duration_seconds": 28.5,
  "output": "...(Claude Code's conversation output)...",
  "hint": "Use get_task_file to retrieve file contents."
}
```

### list_task_files

Lists all result files from a completed task with their sizes and MIME types.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `task_id` | string | yes | The task ID from run_task |

**Example response:**
```json
[
  {"name": "index.html", "size": 4521, "mime_type": "text/html"},
  {"name": "screenshot.png", "size": 37519, "mime_type": "image/png"},
  {"name": "app.js", "size": 1283, "mime_type": "application/javascript"}
]
```

### get_task_file

Retrieves the actual contents of a result file. File type determines how it's returned:

| File Type | Returned As | Claude Can... |
|-----------|-------------|---------------|
| Text/code (.html, .js, .py, .go, .md, etc.) | Plain text | Read and use the code |
| Images (.png, .jpg, .gif, .webp, etc.) | MCP image content (base64) | View the image |
| Other binary files | Base64-encoded JSON | Decode if needed |

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `task_id` | string | yes | The task ID |
| `filename` | string | yes | Filename from list_task_files |

### get_task_status

Check if a task is still running and get its current output.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `task_id` | string | yes | The task ID |

### list_vms

List all currently running MicroVMs. No parameters.

### exec_in_vm

Execute a shell command inside a running VM (useful if `auto_destroy` was set to false).

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `vm_name` | string | yes | Name of the VM |
| `command` | string | yes | Shell command to execute |

### read_vm_file

Read a file from inside a running VM.

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `vm_name` | string | yes | Name of the VM |
| `path` | string | yes | Absolute path inside the VM |

### destroy_vm

Stop and destroy a VM, cleaning up all resources (TAP device, iptables rules, jailer chroot, state files).

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `vm_name` | string | yes | Name of the VM to destroy |

## Typical Workflows

### Code Generation + Review

```
User: "Use the orchestrator to build a REST API in Go with user CRUD endpoints"

Claude Code:
  1. run_task(prompt="Build a Go REST API with user CRUD...")
     → result_files: ["main.go", "handlers.go", "models.go", "go.mod"]
  2. get_task_file(task_id="xxx", filename="main.go")
     → returns Go source code
  3. get_task_file(task_id="xxx", filename="handlers.go")
     → returns Go source code
  4. Shows the code to the user
```

### Screenshot + Visual Verification

```
User: "Use the orchestrator to take a screenshot of google.com"

Claude Code:
  1. run_task(prompt="Take a screenshot of google.com using headless chromium...")
     → result_files: ["screenshot.png"]
  2. get_task_file(task_id="xxx", filename="screenshot.png")
     → returns image (Claude can see it)
  3. Describes what's in the screenshot
```

### Build + Test + Collect Results

```
User: "Use the orchestrator to create a todo app, run it, and take screenshots"

Claude Code:
  1. run_task(prompt="Create a todo app, start it, take screenshots...")
     → result_files: ["index.html", "app.js", "screenshot-empty.png", "screenshot-with-todos.png"]
  2. get_task_file(..., "index.html") → HTML code
  3. get_task_file(..., "screenshot-with-todos.png") → viewable image
  4. Shows everything to the user
```

### Long-Running Task with Status Check

```
User: "Use the orchestrator to run a comprehensive test suite"

Claude Code:
  1. run_task(prompt="Clone repo X, install deps, run full test suite...", timeout=300)
     → (blocks until complete or timeout)
  2. If needed, check: get_task_status(task_id="xxx")
  3. get_task_file(task_id="xxx", filename="test-results.txt")
```

## Local Mode (stdio)

For Claude Code running on the same machine as the orchestrator, use stdio transport instead of HTTP:

Add to `~/.claude/mcp.json`:

```json
{
  "mcpServers": {
    "orchestrator": {
      "command": "sudo",
      "args": ["/path/to/bin/orchestrator", "mcp"]
    }
  }
}
```

This avoids network overhead and doesn't require port 8081 to be open.

## Resource Limits

The server has 30GB RAM with ~26GB available for VMs. Each task creates a VM:

| VM Size | RAM | vCPUs | Max Concurrent Tasks |
|---------|-----|-------|---------------------|
| Default | 2048 MB | 2 | ~12 |
| Large (with browser) | 4096 MB | 2 | ~6 |
| Small (scripts only) | 512 MB | 1 | ~50 |

Tasks have a default timeout of 600 seconds (10 minutes). The VM is always destroyed after the task completes (or times out).

## Security Notes

- Each task runs in a completely isolated MicroVM (KVM hardware virtualization)
- VMs are destroyed after each task — no state leaks between tasks
- Claude Code inside the VM runs with full permissions (`CLAUDE_DANGEROUSLY_SKIP_PERMISSIONS=true`)
- OAuth credentials are injected per-VM from the server's `~/.claude/.credentials.json`
- The MCP server binds to `127.0.0.1` by default. To expose on LAN you must explicitly set `--addr 0.0.0.0:...`, and when binding non-loopback an auth token is **required** (either via `--auth-token` or auto-generated and printed at startup).
- Clients must send `Authorization: Bearer <token>` on every request to a non-loopback-bound server.
