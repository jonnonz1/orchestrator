# Snapshots & warm pool

Cold boot is ~4 seconds. That's fine for a CLI task, snappy for an
interactive dashboard, and too slow if you're serving many short tasks via
MCP. Snapshots fix that: after a VM has reached the state you want,
Firecracker can dump its memory and register state to disk, and restoring
from that snapshot boots a clone in ~150 ms.

## How snapshots work

Firecracker's snapshotting is a two-file dump:

- **Memory** (`<name>.memory`) — the full guest RAM as of the snapshot.
- **State** (`<name>.state`) — VM register + device state.

To create one we:

1. Pause the VM via the Firecracker API (`PATCH /vm` `{"state":"Paused"}`).
2. Trigger `PUT /snapshot/create` with paths on the host.
3. Optionally resume (`--resume`) or destroy the paused VM.

To restore:

1. Create a new VM's chroot and copy the snapshot files in.
2. Configure the new VM to network exactly like the source (same TAP, or a
   freshly-allocated one if you care about isolation).
3. `PUT /snapshot/load` with the snapshot paths.
4. Resume the VM.

Orchestrator handles all that via `orchestrator snapshot create|restore|list|delete`.

## When to use snapshots

**Use snapshots when:**

- You have a task shape that runs thousands of times per day and each cold
  boot is pure overhead.
- You want sub-second task starts for a demo.
- The workload benefits from a warm cache (npm packages already installed,
  playwright browsers pre-downloaded, etc.).

**Don't use snapshots when:**

- You want every task to start from a cryptographically clean slate (memory
  is carried over — a malicious previous tenant could leave artefacts in a
  shared snapshot).
- You only run a handful of tasks per day — the complexity isn't worth it.

## CLI workflow

Create a snapshot from a running VM, leaving the source running:

```bash
sudo ./bin/orchestrator snapshot create \
  --vm my-vm \
  --name warm-v1 \
  --resume
```

Restore into a new VM named `task-42`:

```bash
sudo ./bin/orchestrator snapshot restore \
  --name warm-v1 \
  --vm task-42
```

List + delete:

```bash
sudo ./bin/orchestrator snapshot list
sudo ./bin/orchestrator snapshot delete --name warm-v1
```

## Building a warm pool

The typical pattern: boot a "template" VM, run the setup commands that are
the same for every task (install deps, warm caches), snapshot, then on each
incoming task restore into a fresh VM, inject the per-task prompt, and run.

Rough recipe (no code yet in-repo; planned for a future release):

1. Boot template VM: `vm create --name template-node`
2. Exec setup: `vm exec --name template-node --command "npm install -g tsx"`
3. Snapshot: `snapshot create --vm template-node --name node-warm`
4. At task time, `snapshot restore` instead of `vm create`; everything else
   about task runner stays the same.

## Security considerations

A restored VM is memory-equivalent to the snapshot. If the snapshot contained
secrets (OAuth tokens, API keys), **they're still in memory of the clone**.
Snapshots should be treated like rootfs images:

- Store them in a directory root can read but nothing else.
- Never snapshot a VM that had a task running with credentials.
- Prefer snapshotting a pristine template (just-installed packages, no
  credentials injected) and inject credentials at restore time.

## Performance

Numbers from the reference box (Ryzen 5 5600GT, 30 GB RAM):

| Operation | Time |
|---|---|
| Snapshot create (paused, 2 GB VM) | ~300 ms |
| Snapshot restore (2 GB VM) | ~150 ms |
| Snapshot file size | ~same as VM's committed memory |

The 2 GB default VM takes ~2 GB of snapshot disk. Plan capacity accordingly.
