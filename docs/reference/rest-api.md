# REST API Reference

Base URL: `http://<your-server>:8080/api/v1` (defaults to `127.0.0.1:8080`)

## Health & Stats

### GET /health

Health check.

```bash
curl http://localhost:8080/api/v1/health
```

```json
{"status": "ok"}
```

### GET /stats

System statistics.

```bash
curl http://localhost:8080/api/v1/stats
```

```json
{
  "total_vms": 2,
  "running_vms": 1,
  "total_tasks": 5
}
```

---

## VM Management

### POST /vms

Create and start a new MicroVM.

**Request:**
```json
{
  "name": "my-vm",
  "ram_mb": 2048,
  "vcpus": 2
}
```

**Response (201):**
```json
{
  "name": "my-vm",
  "pid": 12345,
  "ram_mb": 2048,
  "vcpus": 2,
  "vsock_cid": 51107,
  "tap_dev": "fc-my-vm",
  "tap_ip": "172.16.225.1",
  "guest_ip": "172.16.225.2",
  "subnet": "172.16.225.0/24",
  "host_iface": "wlp4s0",
  "jail_id": "my-vm",
  "jailer_base": "/srv/jailer/firecracker/my-vm",
  "state": "running",
  "launched_at": "2026-03-23T11:11:55.738+13:00"
}
```

### GET /vms

List all VMs.

```bash
curl http://localhost:8080/api/v1/vms
```

### GET /vms/{name}

Get details for a specific VM.

```bash
curl http://localhost:8080/api/v1/vms/my-vm
```

### DELETE /vms/{name}

Stop and destroy a VM. Removes all resources (TAP device, iptables rules, chroot, state).

```bash
curl -X DELETE http://localhost:8080/api/v1/vms/my-vm
# Returns 204 No Content
```

### POST /vms/{name}/stop

Stop a VM but keep its state on disk.

```bash
curl -X POST http://localhost:8080/api/v1/vms/my-vm/stop
```

### POST /vms/{name}/exec

Execute a command inside a running VM via the guest agent (vsock).

**Request:**
```json
{
  "command": "hostname && uname -r"
}
```

**Response:**
```json
{
  "exit_code": 0,
  "stdout": "my-vm\n6.1.155+\n"
}
```

---

## Task Management

### POST /tasks

Create and run a Claude Code task. The task runs asynchronously — the response is returned immediately with the task ID.

**Request:**
```json
{
  "prompt": "Take a screenshot of google.com using headless chromium",
  "ram_mb": 4096,
  "vcpus": 2,
  "timeout": 120,
  "max_turns": 50,
  "auto_destroy": true,
  "output_dir": "/root/output",
  "env_vars": {
    "MY_VAR": "value"
  },
  "files": {
    "/root/config.json": "{\"key\": \"value\"}"
  }
}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `prompt` | string | required | Prompt for Claude Code |
| `ram_mb` | int | 2048 | VM RAM in MB |
| `vcpus` | int | 2 | VM vCPUs |
| `timeout` | int | 600 | Timeout in seconds |
| `max_turns` | int | 0 (unlimited) | Max Claude Code turns |
| `auto_destroy` | bool | true | Destroy VM when task completes |
| `output_dir` | string | /root/output | Guest dir to collect results from |
| `env_vars` | object | null | Extra environment variables |
| `files` | object | null | Files to inject (guest_path → content) |

**Response (202):**
```json
{
  "id": "a3bfca80",
  "status": "pending",
  "prompt": "Take a screenshot...",
  "vm_name": "task-a3bfca80",
  "ram_mb": 4096,
  "vcpus": 2,
  "auto_destroy": true,
  "output_dir": "/root/output",
  "timeout": 120,
  "created_at": "2026-03-23T11:20:06Z"
}
```

### GET /tasks

List all tasks.

```bash
curl http://localhost:8080/api/v1/tasks
```

### GET /tasks/{id}

Get task details including output and result files.

```bash
curl http://localhost:8080/api/v1/tasks/a3bfca80
```

**Response:**
```json
{
  "id": "a3bfca80",
  "status": "completed",
  "prompt": "Take a screenshot...",
  "vm_name": "task-a3bfca80",
  "output": "...(Claude Code stream-json output)...",
  "exit_code": 0,
  "result_files": ["screenshot.png"],
  "cost_usd": 0.1581,
  "created_at": "2026-03-23T11:20:06Z",
  "started_at": "2026-03-23T11:20:06Z",
  "completed_at": "2026-03-23T11:20:28Z"
}
```

Task status values: `pending`, `running`, `completed`, `failed`, `cancelled`.

### DELETE /tasks/{id}

Cancel a running task. Destroys the VM if still running.

```bash
curl -X DELETE http://localhost:8080/api/v1/tasks/a3bfca80
# Returns 204 No Content
```

### GET /tasks/{id}/files

List result files from a completed task.

```bash
curl http://localhost:8080/api/v1/tasks/a3bfca80/files
```

```json
[
  {"name": "screenshot.png", "url": "/api/v1/tasks/a3bfca80/files/screenshot.png", "size": 37519}
]
```

### GET /tasks/{id}/files/{filename}

Download a result file. Served with the correct Content-Type.

```bash
# Download a screenshot
curl -o screenshot.png http://localhost:8080/api/v1/tasks/a3bfca80/files/screenshot.png

# View code
curl http://localhost:8080/api/v1/tasks/a3bfca80/files/index.html
```

---

## WebSocket Streaming

### GET /tasks/{id}/stream

WebSocket endpoint for streaming Claude Code output in real-time while a task is running.

**Connect:**
```javascript
const ws = new WebSocket('ws://localhost:8080/api/v1/tasks/a3bfca80/stream');
ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  console.log(msg.type, msg.data);
};
```

**Message format:**
```json
{
  "type": "stdout",
  "data": "{\"type\":\"assistant\",\"message\":{...}}",
  "timestamp": "2026-03-23T11:20:10Z"
}
```

Types: `stdout`, `stderr`, `exit`.

On connect, buffered history (up to 1000 lines) is replayed, then live events follow. The connection closes after the `exit` event.

---

## Web Dashboard

The web dashboard is served at the root URL (`http://localhost:8080/`). It provides:

- **Dashboard** — Overview stats, quick task creation, recent tasks and running VMs
- **VMs page** — Create, list, and destroy VMs
- **Tasks page** — List all tasks with status
- **Task Detail** — Human-readable parsed output stream, result file list with download links, inline image previews

The frontend is embedded in the Go binary and requires no separate server.
