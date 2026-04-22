# Firecracker MicroVM Platform — Go Orchestrator Reference

## Table of Contents

1. [Architecture Overview](#1-architecture-overview)
2. [Directory Layout & File Inventory](#2-directory-layout--file-inventory)
3. [VM Lifecycle](#3-vm-lifecycle)
4. [Firecracker REST API (Full Reference)](#4-firecracker-rest-api)
5. [Networking Architecture](#5-networking-architecture)
6. [Vsock Host<->Guest Communication](#6-vsock-hostguest-communication)
7. [Context Injection Patterns](#7-context-injection-patterns)
8. [Running Claude Code Inside a MicroVM](#8-running-claude-code-inside-a-microvm)
9. [Headless Browser Setup](#9-headless-browser-setup)
10. [Go Orchestrator Design Guide](#10-go-orchestrator-design-guide)
11. [Resource Limits & Capacity Planning](#11-resource-limits--capacity-planning)
12. [Security Considerations](#12-security-considerations)
13. [Snapshots & Fast Restore](#13-snapshots--fast-restore)
14. [Troubleshooting](#14-troubleshooting)

---

## 1. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                        HOST (Ubuntu 24.04)                         │
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐   │
│  │                   Go Orchestrator (you build)                │   │
│  │                                                              │   │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────────────────────┐   │   │
│  │  │ VM Pool  │  │ Network  │  │ Context Injection Engine  │   │   │
│  │  │ Manager  │  │ Manager  │  │ (rootfs mount / vsock /   │   │   │
│  │  │          │  │          │  │  MMDS / SSH)               │   │   │
│  │  └────┬─────┘  └────┬─────┘  └────────────┬──────────────┘   │   │
│  └───────┼──────────────┼─────────────────────┼──────────────────┘   │
│          │              │                     │                       │
│  ┌───────▼──────────────▼─────────────────────▼──────────────────┐   │
│  │                    Jailer (per VM)                             │   │
│  │   chroot: /srv/jailer/firecracker/<id>/root/                  │   │
│  │   ┌─────────────────────────────────────────────────────┐     │   │
│  │   │              Firecracker VMM Process                │     │   │
│  │   │   API Socket: /run/firecracker.socket (UDS)         │     │   │
│  │   │   ┌───────────────────────────────────────────┐     │     │   │
│  │   │   │            microVM (KVM guest)            │     │     │   │
│  │   │   │                                           │     │     │   │
│  │   │   │  Kernel: vmlinux-6.1.155                  │     │     │   │
│  │   │   │  Rootfs: Debian Bookworm (ext4)           │     │     │   │
│  │   │   │  Network: eth0 → TAP → NAT → internet    │     │     │   │
│  │   │   │  Vsock: CID N → /vsock.sock on host       │     │     │   │
│  │   │   │                                           │     │     │   │
│  │   │   │  Contains: python3, node, npm, git,       │     │     │   │
│  │   │   │  curl, wget, jq, ssh, networking tools    │     │     │   │
│  │   │   └───────────────────────────────────────────┘     │     │   │
│  │   └─────────────────────────────────────────────────────┘     │   │
│  └───────────────────────────────────────────────────────────────┘   │
│                                                                       │
│  /dev/kvm ←── KVM (kvm_amd module)                                    │
│  cgroups v2 ←── /sys/fs/cgroup                                        │
└───────────────────────────────────────────────────────────────────────┘
```

### Key Properties of Firecracker MicroVMs

| Property | Value |
|----------|-------|
| Boot time | ~125ms (kernel + init) |
| Memory overhead | ~5MB per VM beyond guest RAM |
| Max VMs per host | Limited by RAM and file descriptors |
| Isolation | KVM hardware virtualization (not containers) |
| API transport | Unix Domain Socket (UDS), not TCP |
| Supported kernels | 5.10 LTS, 6.1 LTS |
| Max vCPUs per VM | 32 |
| Max RAM per VM | 256 GB |
| Network | virtio-net via TAP devices |
| Block storage | virtio-blk (raw or ext4 images) |
| Host<->Guest comms | Vsock, SSH, MMDS |
| Snapshots | Full VM snapshot + restore |

---

## 2. Directory Layout & File Inventory

```
/opt/firecracker/
├── firecracker                     # Firecracker VMM binary (v1.15.0)
├── jailer                          # Jailer binary (v1.15.0)
├── firecracker-api-v1.15.0.yaml    # OpenAPI spec (copy in ~/firecracker-api-v1.15.0.yaml)
├── kernels/
│   ├── vmlinux -> vmlinux-6.1.155  # Symlink to active kernel
│   └── vmlinux-6.1.155             # Guest kernel (43MB, ELF binary)
├── rootfs/
│   └── base-rootfs.ext4            # Base rootfs image (4GB, ext4)
│                                    #   Debian Bookworm with:
│                                    #   python3 3.11, node 24, npm 11, git,
│                                    #   curl, wget, jq, openssh-server,
│                                    #   iproute2, iputils-ping, net-tools,
│                                    #   dnsutils, traceroute
│                                    #   Root password: "firecracker"
│                                    #   systemd-networkd enabled
│                                    #   SSH enabled (PermitRootLogin yes)
├── scripts/
│   ├── launch-vm.sh                # Launch a microVM (uses jailer)
│   └── teardown-vm.sh              # Stop + cleanup a microVM
└── vms/                            # Per-VM runtime state (created at launch)
    └── <vm-name>/
        ├── rootfs.ext4             # This VM's copy of the rootfs
        ├── metadata.json           # VM config (IPs, CID, PID, etc.)
        └── pid                     # Firecracker PID file

/srv/jailer/firecracker/            # Jailer chroot (managed by jailer)
└── <vm-name>/
    └── root/
        ├── vmlinux                 # Kernel (copied into chroot)
        ├── rootfs.ext4             # Rootfs (copied into chroot)
        ├── vm-config.json          # Firecracker config
        ├── dev/                    # Minimal /dev (created by jailer)
        │   ├── kvm
        │   ├── net/tun
        │   └── urandom
        └── run/
            └── firecracker.socket  # API socket (UDS)
```

### Metadata File Schema (`metadata.json`)

This is what your orchestrator should read/write to track VMs:

```json
{
    "name": "test1",
    "pid": 35274,
    "ram_mb": 1024,
    "vcpus": 2,
    "vsock_cid": 63702,
    "tap_dev": "fc-test1",
    "tap_ip": "172.16.188.1",
    "guest_ip": "172.16.188.2",
    "subnet": "172.16.188.0/24",
    "host_iface": "wlp4s0",
    "jail_id": "test1",
    "jailer_base": "/srv/jailer/firecracker/test1",
    "launched_at": "2026-03-22T20:29:00+00:00"
}
```

---

## 3. VM Lifecycle

### Current Shell Script Interface

Your Go orchestrator will replace these scripts, but they document the exact sequence:

#### Launch Sequence (what `launch-vm.sh` does)

```
1. VALIDATE          Validate name, check /dev/kvm, check base images exist
2. COPY ROOTFS       cp base-rootfs.ext4 → /opt/firecracker/vms/<name>/rootfs.ext4
3. INJECT CONTEXT    Mount rootfs, write network config, hostname, SSH keys
4. SETUP NETWORK     Create TAP device, assign IP, add iptables NAT + FORWARD rules
5. SETUP JAILER      Create chroot at /srv/jailer/firecracker/<id>/root/
                     Copy kernel + rootfs into chroot
6. WRITE CONFIG      Generate vm-config.json inside chroot
7. LAUNCH            jailer --id <id> --exec-file firecracker --daemonize \
                       -- --config-file /vm-config.json --api-sock /run/firecracker.socket
8. RECORD STATE      Write metadata.json + pid file
```

#### Teardown Sequence (what `teardown-vm.sh` does)

```
1. LOAD METADATA     Read metadata.json for TAP dev, subnet, PID, etc.
2. KILL PROCESS      SIGTERM → wait 5s → SIGKILL if still alive
3. REMOVE TAP        ip link del <tap>
4. REMOVE IPTABLES   Delete NAT POSTROUTING + FORWARD rules
5. REMOVE CHROOT     rm -rf /srv/jailer/firecracker/<id>
6. REMOVE STATE      rm -rf /opt/firecracker/vms/<name>
```

### VM States

```
                    ┌──────────┐
            ┌──────►│ CREATING │ (rootfs copy + network setup)
            │       └────┬─────┘
            │            │
            │       ┌────▼─────┐
            │       │ STARTING │ (jailer + firecracker exec)
            │       └────┬─────┘
            │            │
 teardown   │       ┌────▼─────┐
 ───────────┤       │ RUNNING  │ (PID alive, API socket available)
            │       └────┬─────┘
            │            │ kill / API action / guest shutdown
            │       ┌────▼─────┐
            │       │ STOPPED  │ (process exited)
            │       └────┬─────┘
            │            │
            │       ┌────▼──────┐
            └───────│ DESTROYED │ (all resources cleaned up)
                    └───────────┘
```

---

## 4. Firecracker REST API

The API is served over a **Unix Domain Socket** (UDS), not HTTP/TCP. The socket lives inside the jailer chroot at:

```
/srv/jailer/firecracker/<id>/root/run/firecracker.socket
```

### Accessing the API from Go

```go
import (
    "context"
    "net"
    "net/http"
)

func firecrackerHTTPClient(socketPath string) *http.Client {
    return &http.Client{
        Transport: &http.Transport{
            DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
                return net.Dial("unix", socketPath)
            },
        },
    }
}

// Usage:
// client := firecrackerHTTPClient("/srv/jailer/firecracker/test1/root/run/firecracker.socket")
// resp, err := client.Get("http://localhost/")
// resp, err := client.Get("http://localhost/machine-config")
```

### Core API Endpoints

All requests use `Content-Type: application/json`. The base URL is always `http://localhost` (routed via UDS).

#### Instance Info

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/` | Instance info (state, app name, vmm version) |
| `GET` | `/version` | Firecracker version string |
| `GET` | `/vm/config` | Full VM configuration dump |

**Response `GET /`:**
```json
{
    "id": "anonymous-instance",
    "state": "Running",
    "vmm_version": "1.15.0",
    "app_name": "Firecracker"
}
```

#### Actions (VM Control)

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/actions` | Trigger an action on the VM |

**Actions payload:**
```json
// Start the VM (if configured via API rather than --config-file)
{"action_type": "InstanceStart"}

// Send Ctrl+Alt+Del to guest (graceful shutdown)
{"action_type": "SendCtrlAltDel"}

// Flush VM metrics
{"action_type": "FlushMetrics"}
```

**Important:** With `--config-file` (our setup), the VM auto-starts. `InstanceStart` is only needed when configuring via individual API calls.

#### Machine Configuration

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/machine-config` | Current machine config |
| `PUT` | `/machine-config` | Set machine config (pre-boot only) |
| `PATCH` | `/machine-config` | Update machine config (pre-boot only) |

**Response `GET /machine-config`:**
```json
{
    "vcpu_count": 2,
    "mem_size_mib": 1024,
    "smt": false,
    "track_dirty_pages": false,
    "huge_pages": "None"
}
```

#### Boot Source

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/boot-source` | Set kernel + boot args (pre-boot only) |

```json
{
    "kernel_image_path": "/vmlinux",
    "boot_args": "console=ttyS0 reboot=k panic=1 pci=off init=/sbin/init"
}
```

#### Drives (Block Devices)

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/drives/{drive_id}` | Attach a drive (pre-boot) |
| `PATCH` | `/drives/{drive_id}` | Update a drive (hot-swap path while running) |

```json
{
    "drive_id": "rootfs",
    "path_on_host": "/rootfs.ext4",
    "is_root_device": true,
    "is_read_only": false
}
```

**Hot-swap a drive (while VM is running):**
```json
// PATCH /drives/data
{
    "drive_id": "data",
    "path_on_host": "/new-data-disk.ext4"
}
```

This is useful for injecting additional data disks into a running VM.

#### Network Interfaces

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/network-interfaces/{iface_id}` | Attach NIC (pre-boot) |
| `PATCH` | `/network-interfaces/{iface_id}` | Update rate limiters (running) |

```json
{
    "iface_id": "eth0",
    "guest_mac": "06:00:AC:10:00:02",
    "host_dev_name": "fc-test1",
    "rx_rate_limiter": {
        "bandwidth": {"size": 10485760, "refill_time": 1000},
        "ops": {"size": 1000, "refill_time": 1000}
    },
    "tx_rate_limiter": {
        "bandwidth": {"size": 10485760, "refill_time": 1000},
        "ops": {"size": 1000, "refill_time": 1000}
    }
}
```

#### Vsock

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/vsock` | Configure vsock device (pre-boot) |

```json
{
    "guest_cid": 3,
    "uds_path": "/vsock.sock"
}
```

#### MMDS (MicroVM Metadata Service)

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/mmds/config` | Configure MMDS (pre-boot) |
| `PUT` | `/mmds` | Set metadata (any time) |
| `PATCH` | `/mmds` | Update metadata (any time) |
| `GET` | `/mmds` | Read metadata (from host) |

**Configure MMDS:**
```json
// PUT /mmds/config
{
    "version": "V2",
    "network_interfaces": ["eth0"],
    "ipv4_address": "169.254.169.254"
}
```

**Set metadata:**
```json
// PUT /mmds
{
    "task": {
        "id": "abc-123",
        "command": "run-tests",
        "repo": "https://github.com/org/repo.git",
        "branch": "main",
        "timeout_seconds": 300
    },
    "secrets": {
        "github_token": "ghp_xxxx",
        "npm_token": "npm_xxxx"
    }
}
```

**Access from inside the guest:**
```bash
# Guest must first acquire a token (MMDS V2)
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" \
    -H "X-metadata-token-ttl-seconds: 300")

# Then use the token to read metadata
curl -s -H "X-metadata-token: $TOKEN" http://169.254.169.254/task
curl -s -H "X-metadata-token: $TOKEN" http://169.254.169.254/secrets/github_token
```

#### Balloon (Dynamic Memory)

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/balloon` | Create balloon device (pre-boot) |
| `PATCH` | `/balloon` | Resize balloon (running — reclaim/return memory) |
| `GET` | `/balloon` | Current balloon config |
| `GET` | `/balloon/statistics` | Memory statistics |

```json
// PUT /balloon (pre-boot)
{"amount_mib": 0, "deflate_on_oom": true, "stats_polling_interval_s": 5}

// PATCH /balloon (running — inflate to reclaim 256MB)
{"amount_mib": 256}
```

#### Snapshots

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/snapshot/create` | Create a snapshot |
| `PUT` | `/snapshot/load` | Load a snapshot |
| `PATCH` | `/vm` | Pause/resume VM (required for snapshotting) |

```json
// Pause VM
// PATCH /vm
{"state": "Paused"}

// Create snapshot
// PUT /snapshot/create
{
    "snapshot_type": "Full",
    "snapshot_path": "/snapshot.bin",
    "mem_file_path": "/mem.bin"
}

// Resume VM
// PATCH /vm
{"state": "Resumed"}
```

#### Metrics & Logging

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/logger` | Configure logging output |
| `PUT` | `/metrics` | Configure metrics output |

```json
// PUT /logger
{"log_path": "/dev/stderr", "level": "Warning", "show_level": true, "show_log_origin": true}

// PUT /metrics
{"metrics_path": "/metrics.fifo"}
```

#### CPU Configuration

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/cpu-config` | Set CPU template (pre-boot) |

#### Memory Hotplug

| Method | Path | Description |
|--------|------|-------------|
| `PUT` | `/hotplug/memory` | Configure hotplug (pre-boot) |
| `PATCH` | `/hotplug/memory` | Add memory to running VM |
| `GET` | `/hotplug/memory` | Current hotplug config |

### API-Driven Launch (Alternative to --config-file)

For fine-grained control from your orchestrator, configure the VM via individual API calls instead of a config file:

```
1.  PUT /boot-source          ← kernel path + boot args
2.  PUT /drives/rootfs        ← rootfs image
3.  PUT /network-interfaces/eth0  ← TAP device
4.  PUT /vsock                ← vsock config
5.  PUT /machine-config       ← vCPUs + RAM
6.  PUT /mmds/config          ← enable MMDS
7.  PUT /mmds                 ← inject task metadata
8.  PUT /logger               ← optional: logging
9.  PUT /metrics              ← optional: metrics
10. PUT /actions              ← {"action_type": "InstanceStart"}
```

This is the recommended approach for your Go orchestrator — it gives you full control and error handling at each step.

---

## 5. Networking Architecture

### Per-VM Network Topology

```
                 Internet
                    │
              ┌─────▼──────┐
              │  wlp4s0     │  Host's default interface
              │  (or eth0)  │
              └─────┬──────┘
                    │ iptables NAT (MASQUERADE)
                    │
              ┌─────▼──────┐
              │  iptables   │  FORWARD rules per TAP device
              │  FORWARD    │
              └─────┬──────┘
                    │
         ┌──────────┼──────────┐
         │          │          │
    ┌────▼───┐ ┌───▼────┐ ┌───▼────┐
    │fc-test1│ │fc-test2│ │fc-test3│   TAP devices
    │ .16.X.1│ │ .16.Y.1│ │ .16.Z.1│   (host-side IPs)
    └────┬───┘ └───┬────┘ └───┬────┘
         │         │          │
    ┌────▼───┐ ┌───▼────┐ ┌───▼────┐
    │  eth0  │ │  eth0  │ │  eth0  │   Guest NICs (virtio-net)
    │ .16.X.2│ │ .16.Y.2│ │ .16.Z.2│   (guest-side IPs)
    └────────┘ └────────┘ └────────┘
      VM test1   VM test2   VM test3
```

### IP Addressing Scheme

Each VM gets a `/24` subnet derived from a hash of its name:

```
Subnet:    172.16.<slot>.0/24      (slot = hash(name) % 253 + 1)
Host TAP:  172.16.<slot>.1         (gateway for the guest)
Guest:     172.16.<slot>.2         (static IP inside guest)
```

### Network Operations for Go Orchestrator

Your orchestrator will need to perform these operations (currently done by the shell scripts):

```go
// Create TAP device
exec.Command("ip", "tuntap", "add", "dev", tapDev, "mode", "tap").Run()
exec.Command("ip", "addr", "add", tapIP+"/24", "dev", tapDev).Run()
exec.Command("ip", "link", "set", tapDev, "up").Run()

// Enable IP forwarding (do once globally)
exec.Command("sysctl", "-w", "net.ipv4.ip_forward=1").Run()

// NAT
exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING",
    "-s", subnet, "-o", hostIface, "-j", "MASQUERADE").Run()

// FORWARD rules (insert at top to override UFW DROP policy)
exec.Command("iptables", "-I", "FORWARD", "1",
    "-i", tapDev, "-o", hostIface, "-j", "ACCEPT").Run()
exec.Command("iptables", "-I", "FORWARD", "1",
    "-i", hostIface, "-o", tapDev,
    "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
```

Consider using the `github.com/vishvananda/netlink` and `github.com/coreos/go-iptables` Go libraries instead of shelling out.

### Important: UFW Compatibility

This host runs UFW with a default FORWARD DROP policy. The scripts handle this by inserting rules at position 1 in the FORWARD chain. Your orchestrator must do the same — appending rules will not work since UFW's DROP will match first.

---

## 6. Vsock Host<->Guest Communication

Vsock provides a socket-based communication channel between host and guest without using the network stack. It's faster and doesn't require TAP/networking setup.

### How It Works

```
HOST                                          GUEST
────                                          ─────

Connect to UDS:                               Listen on vsock:
/srv/jailer/firecracker/<id>/root/vsock.sock  socket(AF_VSOCK, SOCK_STREAM, 0)
                                              bind(CID=VMADDR_CID_ANY, port=N)

Format: "CONNECT <port>\n"                    Guest sees connections on port N
Then bidirectional data stream                from CID=2 (host)
```

### CID Assignment

| CID | Meaning |
|-----|---------|
| 0 | Hypervisor |
| 1 | Reserved |
| 2 | Host |
| 3+ | Guest VMs (each VM gets a unique CID) |

Our setup auto-assigns CIDs from a hash of the VM name (range 3-65532).

### Host-Side: Connecting via Vsock (Go)

Firecracker exposes vsock as a Unix Domain Socket. To connect from the host:

```go
import (
    "fmt"
    "net"
    "io"
)

func connectVsock(jailID string, guestPort uint32) (net.Conn, error) {
    socketPath := fmt.Sprintf("/srv/jailer/firecracker/%s/root/vsock.sock", jailID)

    conn, err := net.Dial("unix", socketPath)
    if err != nil {
        return nil, fmt.Errorf("dial vsock UDS: %w", err)
    }

    // Send the CONNECT command (Firecracker vsock multiplexing protocol)
    connectCmd := fmt.Sprintf("CONNECT %d\n", guestPort)
    if _, err := conn.Write([]byte(connectCmd)); err != nil {
        conn.Close()
        return nil, fmt.Errorf("vsock CONNECT: %w", err)
    }

    // Read the response ("OK <port>\n" on success)
    buf := make([]byte, 32)
    n, err := conn.Read(buf)
    if err != nil {
        conn.Close()
        return nil, fmt.Errorf("vsock response: %w", err)
    }

    resp := string(buf[:n])
    if resp[:2] != "OK" {
        conn.Close()
        return nil, fmt.Errorf("vsock rejected: %s", resp)
    }

    // conn is now a bidirectional stream to guest port
    return conn, nil
}
```

### Guest-Side: Listening on Vsock (Python example)

```python
#!/usr/bin/env python3
"""Agent that listens on vsock and executes commands."""

import socket
import json
import subprocess

VSOCK_PORT = 9001  # Choose any port

def main():
    sock = socket.socket(socket.AF_VSOCK, socket.SOCK_STREAM)
    sock.bind((socket.VMADDR_CID_ANY, VSOCK_PORT))
    sock.listen(5)
    print(f"Listening on vsock port {VSOCK_PORT}")

    while True:
        conn, addr = sock.accept()
        data = conn.recv(65536).decode()
        request = json.loads(data)

        if request["type"] == "exec":
            result = subprocess.run(
                request["command"],
                shell=True,
                capture_output=True,
                text=True,
                timeout=request.get("timeout", 60)
            )
            response = json.dumps({
                "stdout": result.stdout,
                "stderr": result.stderr,
                "exit_code": result.returncode
            })
            conn.sendall(response.encode())

        conn.close()

if __name__ == "__main__":
    main()
```

### Vsock Use Cases for Your Orchestrator

1. **Command execution** — Run commands inside the VM without SSH overhead
2. **File transfer** — Stream files in/out without SCP
3. **Health checks** — Fast ping/pong without network stack
4. **Log streaming** — Real-time log forwarding from guest agent
5. **Claude Code I/O** — Pipe Claude Code stdin/stdout over vsock

---

## 7. Context Injection Patterns

There are 4 ways to inject context (files, configs, secrets, tasks) into a VM. Choose based on timing and security needs.

### Pattern 1: Rootfs Mount (Pre-Boot)

**When:** Before the VM starts. Best for static context that doesn't change.

```go
func injectViaRootfs(rootfsPath string, files map[string][]byte) error {
    mountDir, _ := os.MkdirTemp("", "fc-mount-")
    defer os.RemoveAll(mountDir)

    // Mount the rootfs image
    exec.Command("mount", rootfsPath, mountDir).Run()
    defer exec.Command("umount", mountDir).Run()

    // Write files into the mounted filesystem
    for guestPath, content := range files {
        fullPath := filepath.Join(mountDir, guestPath)
        os.MkdirAll(filepath.Dir(fullPath), 0755)
        os.WriteFile(fullPath, content, 0644)
    }

    return nil
}

// Example: inject a task definition + SSH keys + git config
injectViaRootfs(rootfsPath, map[string][]byte{
    "/root/task.json":            taskJSON,
    "/root/.ssh/authorized_keys": pubKey,
    "/root/.gitconfig":           gitConfig,
    "/root/setup.sh":             setupScript,
    "/etc/environment":           envVars,
})
```

**Pros:** Simple, no guest agent needed, works before boot.
**Cons:** Requires mounting (needs root), can't update after boot.

### Pattern 2: MMDS (Metadata Service — Pre/Post Boot)

**When:** For task metadata, secrets, dynamic config. Can be updated while VM is running.

```go
func injectViaMmds(apiSocket string, metadata interface{}) error {
    client := firecrackerHTTPClient(apiSocket)

    // First configure MMDS (pre-boot only)
    configBody, _ := json.Marshal(map[string]interface{}{
        "version":            "V2",
        "network_interfaces": []string{"eth0"},
        "ipv4_address":       "169.254.169.254",
    })
    client.Do(newReq("PUT", "/mmds/config", configBody))

    // Set metadata (can be done any time)
    metaBody, _ := json.Marshal(metadata)
    client.Do(newReq("PUT", "/mmds", metaBody))

    return nil
}

// Update metadata on a running VM:
func updateMmds(apiSocket string, patch interface{}) error {
    client := firecrackerHTTPClient(apiSocket)
    body, _ := json.Marshal(patch)
    client.Do(newReq("PATCH", "/mmds", body))
    return nil
}
```

**Guest-side retrieval:**
```bash
# In the VM's init/setup script:
TOKEN=$(curl -s -X PUT http://169.254.169.254/latest/api/token \
    -H "X-metadata-token-ttl-seconds: 300")
TASK=$(curl -s -H "X-metadata-token: $TOKEN" http://169.254.169.254/task)
REPO=$(echo "$TASK" | jq -r '.repo')
git clone "$REPO" /workspace
```

**Pros:** AWS-style, familiar pattern. Updateable at runtime. No mount needed.
**Cons:** Requires MMDS config pre-boot. Limited to metadata (not large files).

### Pattern 3: Vsock Agent (Post-Boot)

**When:** For dynamic interaction after boot. Best for orchestrator<->guest communication.

```go
func injectViaVsock(jailID string, port uint32, files map[string][]byte) error {
    conn, err := connectVsock(jailID, port)
    if err != nil {
        return err
    }
    defer conn.Close()

    request := map[string]interface{}{
        "type":  "write_files",
        "files": files,  // base64-encoded in practice
    }
    data, _ := json.Marshal(request)
    conn.Write(data)

    // Read response
    buf := make([]byte, 4096)
    n, _ := conn.Read(buf)
    fmt.Println("Response:", string(buf[:n]))
    return nil
}
```

**Pros:** Fast, no network overhead, bidirectional, streaming.
**Cons:** Requires a guest agent to be running.

### Pattern 4: SSH/SCP (Post-Boot)

**When:** For ad-hoc operations, debugging, or when you don't want a custom agent.

```go
func injectViaSSH(guestIP string, files map[string][]byte) error {
    config := &ssh.ClientConfig{
        User:            "root",
        Auth:            []ssh.AuthMethod{ssh.Password("firecracker")},
        HostKeyCallback: ssh.InsecureIgnoreHostKey(),
    }
    client, _ := ssh.Dial("tcp", guestIP+":22", config)
    defer client.Close()

    sftpClient, _ := sftp.NewClient(client)
    defer sftpClient.Close()

    for path, content := range files {
        f, _ := sftpClient.Create(path)
        f.Write(content)
        f.Close()
    }
    return nil
}
```

**Pros:** Standard tooling, no custom agent, works with any SSH client.
**Cons:** Slower, requires network stack, password/key management.

### Recommended Pattern for Your Use Case

For running Claude Code tasks inside VMs:

```
1. PRE-BOOT:  Rootfs mount → inject SSH keys, base configs, guest agent script
2. PRE-BOOT:  MMDS config → enable metadata service
3. BOOT:      Start VM
4. POST-BOOT: MMDS PUT → inject task definition (repo, branch, command)
5. POST-BOOT: Vsock → stream Claude Code I/O bidirectionally
6. RUNTIME:   MMDS PATCH → update task state, inject additional context
7. RUNTIME:   Vsock → stream logs, results back to orchestrator
```

---

## 8. Running Claude Code Inside a MicroVM

### Prerequisites in the Rootfs

Claude Code requires Node.js (already installed: v24) and npm (already installed: v11). You'll also need to inject an API key.

### Option A: Pre-install Claude Code in the Base Rootfs

Mount the base rootfs and install:

```bash
sudo mount /opt/firecracker/rootfs/base-rootfs.ext4 /mnt
sudo chroot /mnt npm install -g @anthropic-ai/claude-code
sudo umount /mnt
```

Then inject the API key at launch time via MMDS or rootfs mount:

```go
// Via rootfs mount (pre-boot)
files := map[string][]byte{
    "/etc/environment": []byte("ANTHROPIC_API_KEY=sk-ant-xxx\n"),
}
injectViaRootfs(rootfsPath, files)
```

### Option B: Install at Boot via Init Script

Inject a setup script that runs on first boot:

```bash
#!/bin/bash
# /root/setup.sh — injected into rootfs pre-boot

# Get API key from MMDS
TOKEN=$(curl -s -X PUT http://169.254.169.254/latest/api/token \
    -H "X-metadata-token-ttl-seconds: 300")
export ANTHROPIC_API_KEY=$(curl -s -H "X-metadata-token: $TOKEN" \
    http://169.254.169.254/secrets/anthropic_api_key)

# Install Claude Code (skip if pre-installed in base image)
npm install -g @anthropic-ai/claude-code 2>/dev/null

# Clone the target repo
REPO=$(curl -s -H "X-metadata-token: $TOKEN" http://169.254.169.254/task/repo)
BRANCH=$(curl -s -H "X-metadata-token: $TOKEN" http://169.254.169.254/task/branch)
git clone -b "$BRANCH" "$REPO" /workspace
cd /workspace

# Run Claude Code non-interactively
PROMPT=$(curl -s -H "X-metadata-token: $TOKEN" http://169.254.169.254/task/prompt)
claude -p "$PROMPT" --output-format json > /root/result.json 2>&1

# Signal completion via vsock or write result to a known location
```

### Option C: Stream Claude Code I/O via Vsock (Recommended)

This gives your orchestrator real-time control over Claude Code:

```
Host (Go Orchestrator)                  Guest (microVM)
─────────────────────                   ──────────────────

vsock connect(port 9001) ──────────────► Guest agent listening

Send: {"type": "exec",                  Agent receives command
       "command": "claude -p '...'      Agent spawns Claude Code
           --output-format stream-json",
       "env": {"ANTHROPIC_API_KEY":     Pipes Claude stdout back
               "sk-ant-xxx"}}            over vsock

◄────────────── stream stdout/stderr ──
◄────────────── stream stdout/stderr ──
◄────────────── stream stdout/stderr ──

◄────────────── {"exit_code": 0} ──────
```

### Claude Code CLI Flags Reference (for non-interactive use)

```bash
# Run with a prompt (non-interactive)
claude -p "fix the bug in main.go" --output-format json

# Run with a prompt, streaming output
claude -p "add error handling" --output-format stream-json

# Continue a conversation
claude -p "now add tests" --continue --output-format json

# With specific allowed tools
claude -p "review this PR" --allowedTools "Read,Grep,Glob"

# With max turns
claude -p "refactor auth module" --max-turns 20

# Pipe input
echo "explain this code" | claude -p - --output-format json
```

---

## 9. Headless Browser Setup

### Adding Chromium to the Base Rootfs

```bash
sudo mount /opt/firecracker/rootfs/base-rootfs.ext4 /mnt

sudo chroot /mnt bash -c '
apt-get update
apt-get install -y chromium chromium-sandbox \
    fonts-liberation libgbm1 libnss3 libxss1 \
    libasound2 libatk-bridge2.0-0 libgtk-3-0
'

# Or install via npm (Puppeteer bundles its own Chromium)
sudo chroot /mnt bash -c 'npm install -g puppeteer'

sudo umount /mnt
```

**Note:** This will increase the rootfs size significantly (~300-500MB). Consider making a separate rootfs variant for browser tasks:

```bash
cp /opt/firecracker/rootfs/base-rootfs.ext4 /opt/firecracker/rootfs/browser-rootfs.ext4
# Then install Chromium only in browser-rootfs.ext4
```

### Running Headless Chrome Inside the VM

```bash
# Direct CLI
chromium --headless --no-sandbox --disable-gpu \
    --dump-dom https://example.com

# With Puppeteer (Node.js)
node -e "
const puppeteer = require('puppeteer');
(async () => {
    const browser = await puppeteer.launch({
        headless: true,
        args: ['--no-sandbox', '--disable-setuid-sandbox']
    });
    const page = await browser.newPage();
    await page.goto('https://example.com');
    console.log(await page.content());
    await browser.close();
})();
"
```

### RAM Considerations for Browser VMs

Chromium is memory-hungry. Recommended minimums:

| Use Case | RAM | vCPUs |
|----------|-----|-------|
| Simple page loads / scraping | 1024 MB | 1 |
| Complex SPAs / JS-heavy pages | 2048 MB | 2 |
| Multiple tabs / parallel browsing | 4096 MB | 2-4 |
| Claude Code + browser | 4096 MB | 2-4 |

---

## 10. Go Orchestrator Design Guide

### Recommended Architecture

```go
package orchestrator

// Core types — map to the VM lifecycle and metadata
type VMConfig struct {
    Name    string `json:"name"`
    RamMB   int    `json:"ram_mb"`
    VCPUs   int    `json:"vcpus"`
    Rootfs  string `json:"rootfs"`   // "base" or "browser"
    CID     uint32 `json:"cid"`      // 0 = auto-assign
}

type VMInstance struct {
    Config      VMConfig          `json:"config"`
    PID         int               `json:"pid"`
    GuestIP     string            `json:"guest_ip"`
    TapDev      string            `json:"tap_dev"`
    TapIP       string            `json:"tap_ip"`
    Subnet      string            `json:"subnet"`
    VsockCID    uint32            `json:"vsock_cid"`
    JailID      string            `json:"jail_id"`
    JailerBase  string            `json:"jailer_base"`
    APISocket   string            `json:"api_socket"`
    State       VMState           `json:"state"`
    LaunchedAt  time.Time         `json:"launched_at"`
}

type VMState string
const (
    VMStateCreating  VMState = "creating"
    VMStateStarting  VMState = "starting"
    VMStateRunning   VMState = "running"
    VMStateStopped   VMState = "stopped"
    VMStateDestroyed VMState = "destroyed"
    VMStateError     VMState = "error"
)
```

### Key Interfaces

```go
// VMManager — core lifecycle management
type VMManager interface {
    Create(ctx context.Context, config VMConfig) (*VMInstance, error)
    Start(ctx context.Context, vm *VMInstance) error
    Stop(ctx context.Context, vm *VMInstance) error
    Destroy(ctx context.Context, vm *VMInstance) error
    Get(name string) (*VMInstance, error)
    List() ([]*VMInstance, error)
}

// NetworkManager — TAP device and iptables management
type NetworkManager interface {
    AllocateNetwork(vmName string) (NetworkConfig, error)
    SetupTAP(config NetworkConfig) error
    TeardownTAP(config NetworkConfig) error
    SetupNAT(config NetworkConfig) error
    TeardownNAT(config NetworkConfig) error
}

// ContextInjector — inject context into VMs
type ContextInjector interface {
    // Pre-boot: mount rootfs and write files
    InjectFiles(rootfsPath string, files map[string][]byte) error
    // Pre/post-boot: set MMDS metadata
    SetMetadata(apiSocket string, metadata interface{}) error
    // Post-boot: send data via vsock
    SendVsock(jailID string, port uint32, data []byte) ([]byte, error)
    // Post-boot: execute command via SSH
    ExecSSH(guestIP string, command string) (string, error)
}

// FirecrackerAPI — typed wrapper around the REST API
type FirecrackerAPI interface {
    GetInstanceInfo(socket string) (*InstanceInfo, error)
    SetBootSource(socket string, config BootSource) error
    SetMachineConfig(socket string, config MachineConfig) error
    AddDrive(socket string, drive DriveConfig) error
    AddNetworkInterface(socket string, iface NetworkIfaceConfig) error
    SetVsock(socket string, config VsockConfig) error
    StartInstance(socket string) error
    SendCtrlAltDel(socket string) error
    PauseVM(socket string) error
    ResumeVM(socket string) error
    CreateSnapshot(socket string, config SnapshotConfig) error
    LoadSnapshot(socket string, config SnapshotConfig) error
    ConfigureMMDS(socket string, config MMDSConfig) error
    SetMMDS(socket string, data interface{}) error
    PatchMMDS(socket string, data interface{}) error
}
```

### Existing Go Libraries

| Library | Purpose | Notes |
|---------|---------|-------|
| `github.com/firecracker-microvm/firecracker-go-sdk` | Official Go SDK | Wraps the REST API, manages jailer, handles lifecycle. **Use this.** |
| `github.com/vishvananda/netlink` | TAP/network management | Replace shell `ip` commands |
| `github.com/coreos/go-iptables/iptables` | iptables management | Replace shell `iptables` commands |
| `golang.org/x/crypto/ssh` | SSH client | For post-boot command execution |
| `github.com/pkg/sftp` | SFTP client | For file transfer over SSH |
| `github.com/mdlayher/vsock` | Linux vsock | Native AF_VSOCK support (alternative to UDS) |

### Using the Official Go SDK

The `firecracker-go-sdk` handles most of the complexity:

```go
import (
    firecracker "github.com/firecracker-microvm/firecracker-go-sdk"
    "github.com/firecracker-microvm/firecracker-go-sdk/client/models"
)

func launchVM(ctx context.Context, name string, ramMB int64, vcpus int64) error {
    socketPath := fmt.Sprintf("/tmp/fc-%s.sock", name)

    cfg := firecracker.Config{
        SocketPath:      socketPath,
        KernelImagePath: "/opt/firecracker/kernels/vmlinux",
        KernelArgs:      "console=ttyS0 reboot=k panic=1 pci=off init=/sbin/init",
        Drives: []models.Drive{{
            DriveID:      firecracker.String("rootfs"),
            PathOnHost:   firecracker.String(fmt.Sprintf("/opt/firecracker/vms/%s/rootfs.ext4", name)),
            IsRootDevice: firecracker.Bool(true),
            IsReadOnly:   firecracker.Bool(false),
        }},
        MachineCfg: models.MachineConfiguration{
            VcpuCount:  firecracker.Int64(vcpus),
            MemSizeMib: firecracker.Int64(ramMB),
        },
        NetworkInterfaces: []firecracker.NetworkInterface{{
            StaticConfiguration: &firecracker.StaticNetworkConfiguration{
                MacAddress:  "06:00:AC:10:00:02",
                HostDevName: fmt.Sprintf("fc-%s", name),
            },
        }},
        VsockDevices: []firecracker.VsockDevice{{
            Path: "/vsock.sock",
            CID:  uint32(getCID(name)),
        }},
        JailerCfg: &firecracker.JailerConfig{
            GID:            firecracker.Int(0),
            UID:            firecracker.Int(0),
            ID:             name,
            NumaNode:       firecracker.Int(0),
            ExecFile:       "/opt/firecracker/firecracker",
            JailerBinary:   "/opt/firecracker/jailer",
            CgroupVersion:  "2",
            Daemonize:      true,
        },
    }

    machine, err := firecracker.NewMachine(ctx, cfg)
    if err != nil {
        return err
    }

    return machine.Start(ctx)
}
```

### Orchestrator Launch Flow (API-driven, no config file)

```go
func (o *Orchestrator) LaunchVM(ctx context.Context, task Task) (*VMInstance, error) {
    vm := &VMInstance{Name: task.VMName}

    // 1. Prepare rootfs (copy from base)
    vm.RootfsPath = filepath.Join(o.vmDir, task.VMName, "rootfs.ext4")
    copyFile(o.baseRootfs, vm.RootfsPath)

    // 2. Inject pre-boot context into rootfs
    o.injector.InjectFiles(vm.RootfsPath, map[string][]byte{
        "/root/.ssh/authorized_keys": task.SSHPubKey,
        "/root/task.json":            task.ToJSON(),
        "/etc/environment":           task.EnvFile(),
    })

    // 3. Set up networking
    netCfg, _ := o.netMgr.AllocateNetwork(task.VMName)
    o.netMgr.SetupTAP(netCfg)
    o.netMgr.SetupNAT(netCfg)

    // 4. Set up jailer chroot
    o.setupJailerChroot(vm)

    // 5. Start firecracker via jailer (daemonized)
    o.startFirecracker(vm)

    // 6. Configure VM via API (alternative to --config-file)
    api := o.apiClient(vm.APISocket)
    api.SetBootSource(vm.APISocket, BootSource{...})
    api.AddDrive(vm.APISocket, DriveConfig{...})
    api.AddNetworkInterface(vm.APISocket, NetworkIfaceConfig{...})
    api.SetVsock(vm.APISocket, VsockConfig{...})
    api.SetMachineConfig(vm.APISocket, MachineConfig{...})

    // 7. Configure MMDS
    api.ConfigureMMDS(vm.APISocket, MMDSConfig{...})
    api.SetMMDS(vm.APISocket, task.Metadata)

    // 8. Start the instance
    api.StartInstance(vm.APISocket)

    // 9. Wait for boot + SSH ready
    o.waitForSSH(ctx, vm.GuestIP, 30*time.Second)

    // 10. Post-boot setup via vsock or SSH
    o.injector.ExecSSH(vm.GuestIP, "/root/setup.sh")

    return vm, nil
}
```

---

## 11. Resource Limits & Capacity Planning

### Host Specs

```
CPU:    AMD Ryzen 5 5600GT (6 cores / 12 threads, 2 SMT threads per core)
RAM:    30 GB total (~28 GB available after OS)
Swap:   8 GB
Disk:   407 GB free on /opt
```

### Host Capacity

Reserve ~4GB for host OS + Go orchestrator + buffer. That leaves ~26GB for VMs.
The CPU has 12 threads. Firecracker vCPUs map 1:1 to host threads, so
oversubscribing beyond 10-11 vCPU total will cause contention.

| VM Profile | RAM | vCPUs | Max Concurrent VMs | Total vCPUs | Use Case |
|-----------|-----|-------|--------------------|-------------|----------|
| Tiny | 256 MB | 1 | ~100* | 100* | Simple scripts, health checks |
| Small | 512 MB | 1 | ~52 | 52* | Basic CLI tools, git ops |
| Medium | 1024 MB | 2 | ~26 | 52* | Claude Code, Node.js apps |
| Large | 2048 MB | 2 | ~13 | 26* | Claude Code + npm install |
| XLarge | 4096 MB | 4 | ~6 | 24* | Claude Code + headless browser |

\* vCPU totals exceeding 12 rely on CPU oversubscription. This works for
I/O-bound workloads (waiting on network, API calls) but not for CPU-bound
tasks. For Claude Code (mostly I/O-bound — waiting on API responses), 2-3x
oversubscription is fine. For build/compile tasks, stay at or below 12 total.

**Practical recommendation for Claude Code workloads:** Run 8-12 "Medium"
VMs concurrently (1024MB, 2 vCPUs each = 8-12GB RAM, 16-24 vCPUs with
oversubscription). Claude Code spends most of its time waiting on API
responses, so CPU oversubscription works well here.

### Per-VM Resource Overhead

| Resource | Per VM |
|----------|--------|
| Firecracker process memory | ~5 MB |
| Jailer chroot disk | ~50 MB (kernel) + rootfs copy |
| Rootfs copy | 4 GB (sparse, actual usage ~1-2 GB) |
| TAP device | negligible |
| File descriptors | ~10-15 per VM |

### Optimizing Rootfs Copy Time

The 4GB rootfs copy takes ~2-3 seconds per VM. Options to speed this up:

1. **Use sparse copies:** `cp --sparse=always` (already sparse, but verify)
2. **Use overlayfs:** Mount base as read-only lower, create per-VM upper (fastest, but needs kernel support in guest)
3. **Use device-mapper snapshots:** COW at the block level
4. **Reduce rootfs size:** Strip unnecessary packages, use `--variant=minbase` in debootstrap

### Cgroups v2 Resource Controls

Jailer places each VM in its own cgroup. You can set limits:

```bash
# CPU: limit to 50% of one core
echo 50000 100000 > /sys/fs/cgroup/firecracker/<id>/cpu.max

# Memory: hard limit at 1GB
echo 1073741824 > /sys/fs/cgroup/firecracker/<id>/memory.max

# IO: limit block device bandwidth
echo "8:0 rbps=10485760 wbps=10485760" > /sys/fs/cgroup/firecracker/<id>/io.max
```

Or in Go using the cgroups v2 API:

```go
import "github.com/containerd/cgroups/v3/cgroup2"

mgr, _ := cgroup2.Load(fmt.Sprintf("/firecracker/%s", vmName))
mgr.Update(&cgroup2.Resources{
    CPU: &cgroup2.CPU{
        Max: cgroup2.NewCPUMax(ptr(int64(50000)), ptr(int64(100000))),
    },
    Memory: &cgroup2.Memory{
        Max: ptr(int64(1 << 30)), // 1GB
    },
})
```

---

## 12. Security Considerations

### Current Setup Limitations

| Issue | Current State | Production Recommendation |
|-------|--------------|--------------------------|
| Jailer UID/GID | Running as root (0/0) | Create dedicated `firecracker` user (non-root) |
| Rootfs password | Hardcoded "firecracker" | Use SSH keys only, disable password auth |
| Seccomp filter | Not applied | Use the bundled `seccomp-filter-v1.15.0-x86_64.json` |
| Network isolation | VMs can reach each other | Add iptables rules to block inter-VM traffic |
| API socket | Accessible by root | Set restrictive permissions on socket |

### Hardening for Production

```bash
# 1. Create dedicated user
useradd -r -s /usr/sbin/nologin firecracker
usermod -aG kvm firecracker

# 2. Apply seccomp filter (add to jailer args)
--seccomp-filter /opt/firecracker/seccomp-filter.json

# 3. Block inter-VM traffic
iptables -I FORWARD -s 172.16.0.0/12 -d 172.16.0.0/12 -j DROP

# 4. Rate limit API socket access
chmod 600 /srv/jailer/firecracker/*/root/run/firecracker.socket
```

### Threat Model

| Threat | Mitigation |
|--------|-----------|
| Guest escape via KVM | Firecracker's minimal device model reduces attack surface |
| Guest escape via virtio | Seccomp filter limits syscalls |
| Network-based attacks between VMs | iptables isolation + separate subnets |
| Rootfs tampering | Copy-per-VM pattern (base image is read-only template) |
| Resource exhaustion | Cgroups v2 limits on CPU, memory, I/O |
| API socket hijacking | Jailer chroot + file permissions |

---

## 13. Snapshots & Fast Restore

Snapshots can dramatically reduce VM startup time (from ~2-3s to ~50ms).

### Create a Warm Snapshot

```bash
# 1. Boot a VM normally
sudo /opt/firecracker/scripts/launch-vm.sh --name snapshot-source --ram 1024 --vcpus 2

# 2. Wait for it to fully boot and settle

# 3. Via API: pause, snapshot, resume
SOCKET="/srv/jailer/firecracker/snapshot-source/root/run/firecracker.socket"

# Pause
curl --unix-socket "$SOCKET" -X PATCH http://localhost/vm \
    -H 'Content-Type: application/json' \
    -d '{"state": "Paused"}'

# Create snapshot
curl --unix-socket "$SOCKET" -X PUT http://localhost/snapshot/create \
    -H 'Content-Type: application/json' \
    -d '{"snapshot_type": "Full", "snapshot_path": "/snapshot.bin", "mem_file_path": "/mem.bin"}'

# Copy snapshot files out of jailer chroot
cp /srv/jailer/firecracker/snapshot-source/root/snapshot.bin /opt/firecracker/snapshots/
cp /srv/jailer/firecracker/snapshot-source/root/mem.bin /opt/firecracker/snapshots/
```

### Restore from Snapshot

```go
// In your Go orchestrator:
cfg := firecracker.Config{
    SocketPath: socketPath,
    Snapshot: firecracker.SnapshotConfig{
        SnapshotPath: "/snapshot.bin",
        MemFilePath:  "/mem.bin",
        ResumeVM:     true,
    },
    // Still need drives and network config
    Drives: []models.Drive{...},
    NetworkInterfaces: []firecracker.NetworkInterface{...},
}
```

### Snapshot Strategy for Claude Code VMs

```
1. Boot a "golden" VM with all tools pre-installed
2. Install Claude Code, warm up npm cache
3. Snapshot it → "claude-ready" snapshot
4. For each task: restore from snapshot (~50ms) instead of cold boot (~3s)
5. Inject task context via MMDS or vsock
6. Run task
7. Destroy VM (don't re-snapshot — start fresh each time)
```

---

## 14. Troubleshooting

### Common Issues

| Symptom | Cause | Fix |
|---------|-------|-----|
| `/dev/kvm` missing | KVM module not loaded | `sudo modprobe kvm_amd` |
| Jailer fails with "permission denied" | Bad file permissions | Check firecracker binary has +x |
| VM boots but no network | TAP device not created | Check `ip link show` for TAP device |
| VM has no internet | iptables rules missing or UFW blocking | Check `iptables -L FORWARD -n`, ensure rules are inserted at position 1 |
| VM boots but SSH times out | systemd-networkd not running in guest | Check guest network config in rootfs |
| Vsock connection refused | Guest agent not listening | Verify guest-side listener on correct port |
| API socket not found | Jailer chroot path wrong | Check `/srv/jailer/firecracker/<id>/root/run/firecracker.socket` |
| "Already running" error | Stale PID file | Run teardown first, or check if PID is actually alive |
| Slow rootfs copy | 4GB file copy | Use sparse copies or snapshot-based approach |

### Debugging a Running VM

```bash
# Check VM process
ps aux | grep firecracker

# Check VM state via API
SOCKET="/srv/jailer/firecracker/<name>/root/run/firecracker.socket"
curl --unix-socket "$SOCKET" http://localhost/
curl --unix-socket "$SOCKET" http://localhost/machine-config

# Check guest serial console (if not daemonized)
# The console output goes to the firecracker process stdout

# Check networking from host
ping <guest-ip>
ssh root@<guest-ip>

# Check iptables
iptables -L FORWARD -n -v
iptables -t nat -L POSTROUTING -n -v

# Check TAP device
ip addr show <tap-dev>

# Check jailer chroot contents
ls -la /srv/jailer/firecracker/<name>/root/
```

### Log Locations

| Log | Location |
|-----|----------|
| Jailer launch log | `/opt/firecracker/vms/<name>/jailer.log` |
| Firecracker API log | Configure via `PUT /logger` → log_path |
| Guest syslog | Inside guest: `/var/log/syslog` |
| Guest serial console | Firecracker stdout (lost if daemonized) |
| Kernel messages | Inside guest: `dmesg` |

---

## Appendix: Quick Reference

### Shell Commands

```bash
# Launch a VM
sudo /opt/firecracker/scripts/launch-vm.sh --name myvm --ram 1024 --vcpus 2

# SSH into a VM
ssh root@<guest-ip>   # password: firecracker

# Tear down a VM
sudo /opt/firecracker/scripts/teardown-vm.sh --name myvm

# List running VMs
ls /opt/firecracker/vms/
ps aux | grep firecracker

# Query a VM's API
curl --unix-socket /srv/jailer/firecracker/<name>/root/run/firecracker.socket http://localhost/

# Check VM metadata
cat /opt/firecracker/vms/<name>/metadata.json | jq .
```

### Key Files for Go Orchestrator

```
/opt/firecracker/firecracker           # VMM binary — pass to jailer --exec-file
/opt/firecracker/jailer                 # Jailer binary — your orchestrator calls this
/opt/firecracker/kernels/vmlinux       # Guest kernel — copied into each jailer chroot
/opt/firecracker/rootfs/base-rootfs.ext4  # Base image — copied per VM
~/firecracker-api-v1.15.0.yaml         # OpenAPI spec — generate Go client from this
```

### Go SDK Install

```bash
go get github.com/firecracker-microvm/firecracker-go-sdk
go get github.com/vishvananda/netlink
go get github.com/coreos/go-iptables/iptables
go get github.com/containerd/cgroups/v3
```
