# Python SDK

Source: [`sdk/python/`](https://github.com/jonnonz1/orchestrator/tree/main/sdk/python).

```bash
pip install orchestrator-sdk
```

Or for local development against your checkout:

```bash
pip install -e sdk/python
```

No runtime dependencies — stdlib only.

## Quick example

```python
from orchestrator import Client

client = Client(
    "http://127.0.0.1:8080",
    token="your-bearer-token",  # optional on loopback
)

task = client.run_task(
    prompt="Take a screenshot of https://example.com",
    ram_mb=4096,
    timeout=120,
)

# Stream output as it arrives
for chunk in client.stream(task["id"]):
    print(chunk, end="")

# Or just wait
final = client.wait(task["id"])
print(f"Cost: ${final.get('cost_usd', 0):.4f}")

# Download result files
for f in client.list_files(task["id"]):
    data = client.get_file(task["id"], f["name"])
    open(f["name"], "wb").write(data)
```

## Methods

### VM management

| Method | Wraps |
|---|---|
| `client.list_vms()` | `GET /api/v1/vms` |
| `client.create_vm(name, ram_mb, vcpus)` | `POST /api/v1/vms` |
| `client.destroy_vm(name)` | `DELETE /api/v1/vms/{name}` |

### Tasks

| Method | Wraps |
|---|---|
| `client.run_task(prompt, *, runtime, ram_mb, vcpus, timeout, max_turns)` | `POST /api/v1/tasks` |
| `client.get_task(id)` | `GET /api/v1/tasks/{id}` |
| `client.list_tasks()` | `GET /api/v1/tasks` |
| `client.cancel_task(id)` | `DELETE /api/v1/tasks/{id}` |
| `client.wait(id, poll_interval=1.0, timeout=None)` | Polls `GET /api/v1/tasks/{id}` until terminal. |
| `client.stream(id, poll_interval=0.5)` | Generator over streaming output (polled, not WebSocket). |

### Files

| Method | Wraps |
|---|---|
| `client.list_files(id)` | `GET /api/v1/tasks/{id}/files` |
| `client.get_file(id, filename)` | `GET /api/v1/tasks/{id}/files/{filename}` |

## Error handling

```python
from orchestrator import Client, OrchestratorError

try:
    client.run_task("…")
except OrchestratorError as e:
    print(e.status, e.body)
```

`OrchestratorError.status` is the HTTP status; `OrchestratorError.body` is
the raw response body string.

## Why no `requests` dependency

The SDK is intentionally tiny. It uses `urllib.request` under the hood so
you can drop it into an environment (AWS Lambda, Cloud Functions, a CI
runner) without worrying about extra wheels. For anything that wants async,
`aiohttp` + the OpenAPI spec is two steps away.
