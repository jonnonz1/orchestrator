# orchestrator-sdk (Python)

Python client for the [Orchestrator](../..) MicroVM orchestrator's REST API. Stdlib-only — no `requests`, no async, no ceremony.

## Install

```bash
pip install orchestrator-sdk
```

Or for local development:

```bash
pip install -e sdk/python
```

## Quick example

```python
from orchestrator import Client

client = Client("http://127.0.0.1:8080", token="your-orchestrator-token")

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

## API

- `Client(base_url, token=None, timeout=30.0)`
- `list_vms()`, `create_vm(name, ram_mb, vcpus)`, `destroy_vm(name)`
- `run_task(prompt, *, runtime, ram_mb, vcpus, timeout, max_turns)`
- `get_task(id)`, `list_tasks()`, `cancel_task(id)`, `wait(id, poll_interval, timeout)`
- `list_files(id)`, `get_file(id, filename)`
- `stream(id, poll_interval)` — generator of output chunks

## License

Apache-2.0 (same as Orchestrator).
