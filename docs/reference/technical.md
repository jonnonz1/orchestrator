# Firecracker MicroVM Orchestrator — Technical Documentation

This document covers the full architecture, implementation, setup, and operational details of the orchestrator. Everything described here was built and tested on a single Ubuntu 24.04 machine (AMD Ryzen 5 5600GT, 30GB RAM). The project is approximately 3,200 lines of Go and 860 lines of TypeScript.

---

## Table of Contents

1. [What This Is](#1-what-this-is)
2. [Technology Choices](#2-technology-choices)
3. [Architecture Overview](#3-architecture-overview)
4. [Host Setup and Prerequisites](#4-host-setup-and-prerequisites)
5. [Building the Rootfs Image](#5-building-the-rootfs-image)
6. [Build Process](#6-build-process)
7. [VM Lifecycle — Step by Step](#7-vm-lifecycle--step-by-step)
8. [Networking Deep Dive](#8-networking-deep-dive)
9. [The Guest Agent](#9-the-guest-agent)
10. [The Vsock Communication Layer](#10-the-vsock-communication-layer)
11. [Task Pipeline — End to End](#11-task-pipeline--end-to-end)
12. [Credential Injection and Security](#12-credential-injection-and-security)
13. [Output Streaming](#13-output-streaming)
14. [REST API and WebSocket](#14-rest-api-and-websocket)
15. [Web Dashboard](#15-web-dashboard)
16. [MCP Server](#16-mcp-server)
17. [The SSE to Streamable HTTP Migration](#17-the-sse-to-streamable-http-migration)
18. [CLI Interface](#18-cli-interface)
19. [Known Limitations and Honest Trade-offs](#19-known-limitations-and-honest-trade-offs)

---

## 1. What This Is

A Go program that manages ephemeral Firecracker MicroVMs for running Claude Code tasks in complete isolation. You give it a prompt, it boots a fresh Linux VM in ~4 seconds, runs Claude Code inside it with full permissions, streams the output back, downloads any files Claude produced, destroys the VM, and returns the results.

It was inspired by Anthropic's internal AntSpace platform. The goal was to replicate that isolation model on a single Linux machine without building a complete platform-as-a-service infrastructure.

The project is documented in a 3-part blog series on [jonno.nz](https://jonno.nz):

- [Part 1: Claude Code Running Claude Code in 4-Second Disposable VMs](https://jonno.nz/posts/claude-code-running-claude-code-in-4-second-disposable-vms/) — Motivation, architecture, why Firecracker over Docker
- [Part 2: I Spent 29 Hours Debugging iptables to Boot VMs in 4 Seconds](https://jonno.nz/posts/29-hours-debugging-iptables-to-boot-vms-in-4-seconds/) — Rootfs, networking, guest agent, streaming pipeline
- [Part 3: Claude Code Can Now Spawn Copies of Itself in Isolated VMs](https://jonno.nz/posts/claude-code-can-now-spawn-copies-of-itself-in-isolated-vms/) — MCP server, web dashboard, productionisation

There are three ways to interact with it:

- **CLI** — `sudo ./bin/orchestrator task run --prompt "..."`
- **REST API + Web Dashboard** — `sudo ./bin/orchestrator serve` on port 8080
- **MCP Server** — `sudo ./bin/orchestrator mcp-serve` on port 8081, letting Claude Code on other machines delegate tasks

---

## 2. Technology Choices

### Why Firecracker over Docker/containers

Containers share the host kernel. A kernel vulnerability is a vulnerability in every container on the host. For running Claude Code with `CLAUDE_DANGEROUSLY_SKIP_PERMISSIONS=true` — meaning arbitrary shell commands, arbitrary file writes — that's not acceptable. Firecracker VMs are real KVM-backed virtual machines with their own guest kernel, their own memory space, and hardware-enforced isolation via Intel VT-x or AMD-V. The attack surface is the KVM hypervisor, which is significantly smaller than the container runtime surface.

Anthropic's own sandboxing approach uses OS-level primitives (bubblewrap on Linux, Seatbelt on macOS) for filesystem and network isolation, reporting an 84% reduction in permission prompts internally. This orchestrator goes further by providing full VM-level isolation.

The trade-off is boot time (~4s vs <1s for containers) and disk usage (each VM copies a 4GB rootfs). For tasks that run for 20-120 seconds, the 4-second boot overhead is acceptable.

### Why Go

Go produces static binaries, has excellent concurrency primitives (goroutines for parallel VM management), first-class syscall support (needed for vsock), and compiles in ~2 seconds. The guest agent needs to be a static binary with zero dependencies — `CGO_ENABLED=0` in Go makes this trivial.

### Why vsock over SSH

vsock (AF_VSOCK, address family 40) is a kernel-level host-guest communication channel. It doesn't go through the network stack, doesn't need IP addresses, doesn't need key management, and works even if the guest's network is broken. Firecracker exposes vsock as a Unix domain socket on the host side, which Go can dial directly. SSH would add latency, require key setup, and create an unnecessary network-accessible attack surface inside the VM.

### Why embedded frontend

The React dashboard is compiled to static files and embedded into the Go binary with `//go:embed`. This means the entire system is a single 14MB file you copy to a server. No nginx, no separate frontend deployment, no CORS issues in production.

### Dependencies

Seven direct Go dependencies:

| Dependency | Why |
|---|---|
| `coreos/go-iptables` | Programmatic iptables rule management. The alternative is shelling out to `iptables` which is fragile and hard to make idempotent. |
| `go-chi/chi` | HTTP router. Lightweight, stdlib-compatible, supports URL params and middleware. |
| `go-chi/cors` | CORS middleware. Needed because the dashboard dev server runs on a different port during development. |
| `google/uuid` | Task ID generation. UUIDs truncated to 8 chars for readability. |
| `mark3labs/mcp-go` | MCP protocol implementation. This is the Go SDK for Model Context Protocol servers. |
| `vishvananda/netlink` | TAP device management via netlink. The alternative is shelling out to `ip` commands. |
| `nhooyr.io/websocket` | WebSocket library. Used for real-time streaming of task output to the dashboard. |

Frontend: React 18, TypeScript, Vite, Tailwind CSS.

---

## 3. Architecture Overview

```
┌──────────────────────────────────────────────────────┐
│                    HOST PROCESS                       │
│                                                       │
│  cmd/orchestrator/main.go                             │
│  ┌─────────────────────────────────────────────────┐  │
│  │ internal/api      REST API + WebSocket (:8080)  │  │
│  │ internal/mcp      MCP Server (:8081)            │  │
│  │ internal/vm       VM Manager (create/destroy)   │  │
│  │ internal/network  TAP + iptables                │  │
│  │ internal/task     Task Runner + Store            │  │
│  │ internal/stream   Pub/Sub Hub (ring buffer)     │  │
│  │ internal/inject   Rootfs file injection         │  │
│  │ internal/vsock    Host-side vsock client         │  │
│  │ internal/agent    Wire protocol (shared types)  │  │
│  └─────────────────────────────────────────────────┘  │
└────────────────────────┬─────────────────────────────┘
                         │ vsock (AF_VSOCK via UDS)
┌────────────────────────▼─────────────────────────────┐
│                 GUEST (MicroVM)                        │
│  cmd/agent/main.go                                    │
│  ┌─────────────────────────────────────────────────┐  │
│  │ Guest Agent — listens vsock:9001                │  │
│  │ Handles: ping, exec, write_files, read_file,   │  │
│  │          signal                                 │  │
│  │ Spawns: claude -p "..." --output-format         │  │
│  │         stream-json --verbose                   │  │
│  └─────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────┘
```

The host process and guest agent share `internal/agent/protocol.go` — the wire protocol types and framing functions. This is the only code shared between the two binaries.

---

## 4. Host Setup and Prerequisites

### Hardware

- Linux x86-64 with KVM support (`/dev/kvm` must exist)
- Enough RAM for VMs. Each VM gets dedicated RAM (default 2048MB). With 30GB total, ~26GB is available for VMs after host overhead — enough for 12-13 concurrent VMs at the default 2GB allocation.
- Disk: base rootfs is ~4GB. Each VM copies it (sparse, so actual disk use is lower). Results stored at `/opt/firecracker/results/`.

### Software

- Firecracker v1.15.0 — download the `firecracker` and `jailer` binaries from the [Firecracker releases page](https://github.com/firecracker-microvm/firecracker/releases)
- Go 1.21+ (for building)
- Node.js 18+ (for building the frontend)

### Directory structure

These must exist before first run:

```
/opt/firecracker/
├── firecracker              # Firecracker VMM binary
├── jailer                   # Jailer binary (same release)
├── kernels/
│   └── vmlinux              # Uncompressed guest kernel (we use 6.1.155 LTS)
├── rootfs/
│   └── base-rootfs.ext4     # Base rootfs image (~4GB)
├── vms/                     # Created automatically — per-VM state
└── results/                 # Created automatically — downloaded task files

/srv/jailer/firecracker/     # Created automatically — jailer chroot dirs
```

The paths are hardcoded in `internal/vm/config.go`:

```go
const (
    FCBase     = "/opt/firecracker"
    FCBin      = FCBase + "/firecracker"
    JailerBin  = FCBase + "/jailer"
    KernelPath = FCBase + "/kernels/vmlinux"
    BaseRootfs = FCBase + "/rootfs/base-rootfs.ext4"
    VMDir      = FCBase + "/vms"
    JailerBase = "/srv/jailer/firecracker"
)
```

### Kernel

We use an uncompressed `vmlinux` (not `bzImage`). Firecracker boots vmlinux directly — no bootloader. The boot args are:

```
console=ttyS0 reboot=k panic=1 pci=off init=/sbin/init
```

`pci=off` because Firecracker doesn't emulate PCI. `init=/sbin/init` to boot into systemd.

---

## 5. Building the Rootfs Image

The rootfs is a standard ext4 filesystem image. It's created once, by hand, and then copied (sparse) for each new VM. What needs to be in it:

- **Debian Bookworm** with systemd (for service management and systemd-networkd)
- **Node.js 24** and npm (for Claude Code)
- **Claude Code CLI** (`npm install -g @anthropic-ai/claude-code`)
- **Python 3.11** (Claude Code uses it for some tasks)
- **Chromium** (for browser automation / screenshot tasks)
- **Standard tools**: git, curl, wget, jq, iproute2, dnsutils
- **The guest agent binary**, configured as a systemd service

To inject the agent into the rootfs:

```bash
sudo mount /opt/firecracker/rootfs/base-rootfs.ext4 /mnt
sudo cp bin/agent /mnt/usr/local/bin/agent
sudo chmod +x /mnt/usr/local/bin/agent

# Create the systemd service
sudo tee /mnt/etc/systemd/system/agent.service <<'EOF'
[Unit]
Description=Orchestrator Guest Agent
After=network.target
[Service]
Type=simple
ExecStart=/usr/local/bin/agent
Restart=always
RestartSec=1
[Install]
WantedBy=multi-user.target
EOF

sudo chroot /mnt systemctl enable agent.service
sudo umount /mnt
```

After this, every VM booted from this rootfs will have the agent running on vsock port 9001 within ~1 second of boot.

---

## 6. Build Process

### Makefile

```makefile
build:
	go build -o bin/orchestrator ./cmd/orchestrator

build-agent:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/agent -ldflags="-s -w" ./cmd/agent

all: build build-agent

test:
	go test ./...

clean:
	rm -rf bin/
```

### Frontend build and embedding

```bash
cd web && npm install && npm run build && cd ..
cp -r web/dist cmd/orchestrator/web-dist
go build -o bin/orchestrator ./cmd/orchestrator
```

The `cmd/orchestrator/main.go` uses `//go:embed`:

```go
//go:embed all:web-dist
var webDistEmbed embed.FS
```

When the `serve` command starts, it extracts this embedded filesystem and serves it:

```go
webFS, _ := fs.Sub(webDistEmbed, "web-dist")
api.WebDist = webFS
```

The API server serves unknown paths as `index.html` for SPA client-side routing.

### Agent cross-compilation

The agent must be a static binary because it runs inside a minimal guest. `CGO_ENABLED=0` ensures pure Go compilation. `-ldflags="-s -w"` strips debug info and DWARF tables, reducing the binary from ~3.5MB to ~2.5MB.

---

## 7. VM Lifecycle — Step by Step

`internal/vm/manager.go` — `Create()` method. Everything runs sequentially, with cleanup on failure at each step.

### Step 1: Prepare rootfs

Copy the base image with sparse allocation and inject network config by mounting and writing files:

```go
// Sparse copy — doesn't allocate zero blocks
cmd := exec.Command("cp", "--sparse=always", BaseRootfs, vm.RootfsPath)

// Mount and write systemd-networkd config (static IP, gateway, DNS)
inject.InjectNetworkConfig(vm.RootfsPath, netCfg.GuestIP, netCfg.TapIP, vm.Name)
```

`InjectNetworkConfig` writes three files into the mounted rootfs:
- `/etc/systemd/network/20-eth0.network` — static IP config (no DHCP needed)
- `/etc/resolv.conf` — DNS servers (8.8.8.8, 8.8.4.4)
- `/etc/hostname` — VM name

### Step 2: Setup networking

Create a TAP device and iptables rules (see [Section 8](#8-networking-deep-dive)).

### Step 3: Setup jailer chroot

```go
os.MkdirAll(filepath.Join(vm.JailerPath, "root"), 0755)
exec.Command("cp", KernelPath, filepath.Join(jailerRoot, "vmlinux")).Run()
exec.Command("cp", "--sparse=always", vm.RootfsPath, filepath.Join(jailerRoot, "rootfs.ext4")).Run()
```

The jailer expects the kernel and rootfs to exist inside its chroot. This is a second copy of the rootfs — the first was for network injection, this one is what Firecracker actually uses. The first copy could be eliminated with a refactor, but it hasn't been a bottleneck.

### Step 4: Launch Firecracker via jailer

The VM config is a JSON file written to the jailer chroot:

```go
vmConfig := map[string]interface{}{
    "boot-source": map[string]interface{}{
        "kernel_image_path": "/vmlinux",
        "boot_args":         "console=ttyS0 reboot=k panic=1 pci=off init=/sbin/init",
    },
    "drives": []map[string]interface{}{{
        "drive_id":       "rootfs",
        "path_on_host":   "/rootfs.ext4",
        "is_root_device": true,
        "is_read_only":   false,
    }},
    "machine-config": map[string]interface{}{
        "vcpu_count":  vm.VCPUs,
        "mem_size_mib": vm.RamMB,
    },
    "network-interfaces": []map[string]interface{}{{
        "iface_id":      "eth0",
        "guest_mac":     "06:00:AC:10:00:02",
        "host_dev_name": netCfg.TapDev,
    }},
    "vsock": map[string]interface{}{
        "guest_cid": vm.VsockCID,
        "uds_path":  "/vsock.sock",
    },
}
```

Then launch:

```go
cmd := exec.Command(JailerBin,
    "--id", vm.JailID,
    "--exec-file", FCBin,
    "--uid", "0", "--gid", "0",
    "--cgroup-version", "2",
    "--daemonize",
    "--",
    "--config-file", "/vm-config.json",
    "--api-sock", "/run/firecracker.socket",
)
cmd.Run()
```

The jailer: creates a chroot at `/srv/jailer/firecracker/<id>/root`, minimal `/dev` (kvm, net/tun, urandom), runs Firecracker inside it, and daemonises.

### Step 5: Find PID and persist metadata

After launch there's a 2-second sleep (Firecracker needs time to start), then we find its PID:

```go
out, _ := exec.Command("pgrep", "-f", fmt.Sprintf("firecracker.*--id %s", jailID)).Output()
```

VM metadata (name, PID, IPs, paths) is saved as JSON to `VMDir/<name>/metadata.json`. On orchestrator restart, `recoverState()` reads these files and checks if the process is still alive.

### Destruction

Inverse of creation:

1. Kill the Firecracker process (SIGTERM, wait 5s, SIGKILL)
2. Remove TAP device via netlink
3. Delete iptables NAT and FORWARD rules (best-effort — errors ignored)
4. `os.RemoveAll` the jailer chroot directory
5. `os.RemoveAll` the VM state directory

---

## 8. Networking Deep Dive

### IP allocation

Each VM gets a `/24` subnet derived from its name using FNV-1a hashing:

```go
func HashName(name string) uint32 {
    h := fnv.New32a()
    h.Write([]byte(name))
    return h.Sum32()
}

func NetSlot(name string) int {
    return int(HashName(name)%253) + 1
}

func AllocateNetwork(vmName, hostIface string) NetworkConfig {
    slot := NetSlot(vmName)
    return NetworkConfig{
        TapDev:    TAPName(vmName),
        TapIP:     fmt.Sprintf("172.16.%d.1", slot),   // host side
        GuestIP:   fmt.Sprintf("172.16.%d.2", slot),   // guest side
        Subnet:    fmt.Sprintf("172.16.%d.0/24", slot),
        HostIface: hostIface,
    }
}
```

Example: VM name `task-a3bfca80` → FNV hash → slot 61 → subnet `172.16.61.0/24`, guest IP `172.16.61.2`, host TAP `172.16.61.1`.

This is deterministic and requires no coordination. The collision space is 253 slots, which is sufficient for the expected concurrency (8-12 VMs). In the unlikely event of a collision, one VM's creation would fail because the TAP IP is already assigned. This has never happened in practice.

### TAP device

Linux TAP devices are virtual ethernet interfaces. The host can assign them an IP and route traffic through them. Firecracker attaches its guest `eth0` to the TAP device.

```go
func SetupTAP(cfg NetworkConfig) error {
    tap := &netlink.Tuntap{
        LinkAttrs: netlink.LinkAttrs{Name: cfg.TapDev},
        Mode:      netlink.TUNTAP_MODE_TAP,
    }
    netlink.LinkAdd(tap)
    addr, _ := netlink.ParseAddr(cfg.TapIP + "/24")
    link, _ := netlink.LinkByName(cfg.TapDev)
    netlink.AddrAdd(link, addr)
    netlink.LinkSetUp(link)
    return nil
}
```

TAP names are `fc-<vm-name>`, truncated to 15 characters (Linux interface name limit).

### iptables rules and the UFW problem

For internet access from inside VMs, three iptables rules are needed:

```go
// NAT: rewrite source IP when traffic exits the host
ipt.AppendUnique("nat", "POSTROUTING",
    "-s", cfg.Subnet, "-o", cfg.HostIface, "-j", "MASQUERADE")

// FORWARD: allow outbound from TAP
ipt.Insert("filter", "FORWARD", 1,
    "-i", cfg.TapDev, "-o", cfg.HostIface, "-j", "ACCEPT")

// FORWARD: allow established/related inbound to TAP
ipt.Insert("filter", "FORWARD", 1,
    "-i", cfg.HostIface, "-o", cfg.TapDev,
    "-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT")
```

The FORWARD rules use `Insert` at position 1, not `Append`. This was the single hardest bug in the project — roughly 29 hours of debugging across the first 30 hours of development. The symptom: VMs could resolve DNS but couldn't complete TCP handshakes. Outbound traffic left the host fine (NAT worked), but return traffic was silently dropped. Extensive `tcpdump` analysis eventually revealed the cause.

Ubuntu's UFW adds a blanket `DROP` rule to the FORWARD chain. Using `Append` places custom ACCEPT rules _after_ UFW's DROP, so they never match — packets hit DROP first. Using `Insert` at position 1 places them _before_ UFW's rules, allowing return traffic through.

### Host interface detection

The host's primary internet-facing interface is detected at runtime:

```go
func detectHostInterface() (string, error) {
    out, _ := exec.Command("ip", "route").Output()
    // Parse the "default via X.X.X.X dev <iface>" line
    // Returns something like "wlp4s0" or "eno1"
}
```

### Traffic flow

```
Guest (172.16.61.2) → eth0 → TAP (fc-task-xxx) → iptables FORWARD (ACCEPT)
→ iptables NAT MASQUERADE (rewrite src to host IP) → host interface → internet
→ response comes back → RELATED,ESTABLISHED rule → TAP → guest eth0
```

VMs cannot reach each other. There's no route between 172.16.X.0/24 subnets — each TAP device is point-to-point with its own subnet.

IP forwarding is enabled at the kernel level:

```go
func EnableIPForwarding() error {
    return os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644)
}
```

---

## 9. The Guest Agent

`cmd/agent/main.go` — ~420 lines including the vsock listener.

The agent is a static Go binary that starts via systemd when the VM boots. It listens on vsock port 9001 and handles one request per connection (connect, send request, receive response/stream, disconnect).

### Request types

Defined in `internal/agent/protocol.go`:

```go
const (
    RequestTypeExec       RequestType = "exec"
    RequestTypeWriteFiles RequestType = "write_files"
    RequestTypeReadFile   RequestType = "read_file"
    RequestTypeSignal     RequestType = "signal"
    RequestTypePing       RequestType = "ping"
)
```

### Exec: buffered vs streaming

Buffered exec waits for the command to finish and returns stdout/stderr as strings. Used for short commands like `find` and `ls` during result collection.

Streaming exec is used for running Claude Code. It sends line-by-line events over the vsock connection as the process runs. The protocol:

1. Host sends `exec` request with `stream: true`
2. Agent sends `{"type": "stream"}` response to confirm streaming mode
3. Agent spawns the command, reads stdout/stderr line by line
4. Each line is sent as a `StreamEvent` frame: `{"type": "stdout", "data": "...", "timestamp": "..."}`
5. When the process exits, agent sends `{"type": "exit", "data": "<exit_code>"}`

### The background process problem

Claude Code can start background processes — dev servers, file watchers, etc. These processes inherit stdout/stderr pipes. If the agent waits for pipes to close, it hangs forever because the background processes hold them open.

The solution has three parts:

1. **Process group isolation**: `cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}` puts the command in its own process group.

2. **Wait for process, not pipes**: `cmd.Wait()` returns when the main process exits, regardless of whether children still hold pipes open.

3. **Kill the group**: After the main process exits, `syscall.Kill(-pgid, SIGTERM)` then `SIGKILL` terminates all children, which closes the inherited pipes.

4. **Pipe drain timeout**: Even after killing the group, there's a 3-second timeout waiting for the streaming goroutines to finish. If they're still stuck, we proceed anyway and send the exit event.

```go
// Wait for process exit
<-waitDone

// Kill entire process group
pgid, _ := syscall.Getpgid(cmd.Process.Pid)
syscall.Kill(-pgid, syscall.SIGTERM)
time.Sleep(500 * time.Millisecond)
syscall.Kill(-pgid, syscall.SIGKILL)

// Wait for pipe goroutines with timeout
pipeTimeout := time.After(3 * time.Second)
for i := 0; i < 2; i++ {
    select {
    case <-done:
    case <-pipeTimeout:
        i = 2
    }
}
```

### Line buffering

The streaming pipe reader uses a 256KB buffer per line (`bufio.Scanner`). Claude Code's stream-json output can produce very long single lines (tool results with large file contents).

---

## 10. The Vsock Communication Layer

### Guest side (in the agent)

The agent creates an AF_VSOCK socket using raw syscalls because Go's standard library doesn't support address family 40:

```go
fd, _ := syscall.Socket(40, syscall.SOCK_STREAM, 0)  // AF_VSOCK = 40

// Manually construct struct sockaddr_vm (16 bytes)
sa := [16]byte{}
*(*uint16)(unsafe.Pointer(&sa[0])) = 40          // family
*(*uint32)(unsafe.Pointer(&sa[4])) = uint32(port) // port (9001)
*(*uint32)(unsafe.Pointer(&sa[8])) = 0xFFFFFFFF   // VMADDR_CID_ANY

syscall.RawSyscall(syscall.SYS_BIND, uintptr(fd), uintptr(unsafe.Pointer(&sa[0])), 16)
syscall.RawSyscall(syscall.SYS_LISTEN, uintptr(fd), 5, 0)
```

Accept uses `SYS_ACCEPT4` and wraps the raw file descriptor in a `vsockConn` struct implementing `net.Conn`.

### Host side (vsock client)

Firecracker exposes guest vsock as a Unix domain socket at `/srv/jailer/firecracker/<id>/root/vsock.sock`. The host connects via this UDS and sends a text `CONNECT <port>\n` command to reach the guest port:

```go
func Connect(jailID string, port int) (net.Conn, error) {
    socketPath := fmt.Sprintf("/srv/jailer/firecracker/%s/root/vsock.sock", jailID)
    conn, _ := net.Dial("unix", socketPath)
    conn.Write([]byte(fmt.Sprintf("CONNECT %d\n", port)))
    // Read "OK <port>" response
    return conn, nil
}
```

Each function (`Ping`, `Exec`, `ExecStream`, `WriteFiles`, `ReadFile`) opens a new connection, sends one request, reads the response, and closes. This is connection-per-request, not multiplexed. It's simple and works well because vsock connections are local and fast.

### Wire protocol

Length-prefixed JSON frames:

```
[4 bytes big-endian uint32: payload length][JSON payload]
```

Maximum frame size enforced at 10MB on read:

```go
func ReadFrame(r io.Reader, v interface{}) error {
    var length uint32
    binary.Read(r, binary.BigEndian, &length)
    if length > 10*1024*1024 {
        return fmt.Errorf("frame too large: %d bytes", length)
    }
    data := make([]byte, length)
    io.ReadFull(r, data)
    json.Unmarshal(data, v)
    return nil
}
```

The same `ReadFrame`/`WriteFrame` functions are used by both host and guest — they're in the shared `internal/agent/protocol.go`.

---

## 11. Task Pipeline — End to End

`internal/task/runner.go` — `Run()` method. This is the core of the system.

### Sequence

```
1. Generate task ID (uuid[:8])           → "a3bfca80"
2. Generate VM name                      → "task-a3bfca80"
3. Create VM (see Section 7)             → ~4 seconds
4. Wait for agent (poll vsock ping)      → <1 second
5. Inject context via vsock              → <100ms
6. Run Claude Code via streaming exec    → 20-120 seconds typically
7. Collect result files from VM          → <1 second
8. Destroy VM (if auto_destroy=true)     → <1 second
```

### Step 4: Wait for agent

```go
func (r *Runner) waitForAgent(jailID string, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        _, err := vsock.Ping(jailID)
        if err == nil {
            return nil
        }
        time.Sleep(500 * time.Millisecond)
    }
    return fmt.Errorf("agent did not respond within %s", timeout)
}
```

Polls every 500ms for up to 30 seconds. The agent is typically ready within 1-2 seconds of VM boot.

### Step 5: Inject context

Five categories of files written to the guest via `vsock.WriteFiles`:

1. **Claude credentials** — `/root/.claude/.credentials.json` (mode 0600). Read from the host user's `~/.claude/.credentials.json`. These are OAuth tokens for the Anthropic API.

2. **Claude settings** — `/root/.claude/settings.json`. Allows all tools:
```json
{
    "permissions": {
        "allow": ["Bash(*)", "Read", "Write", "Edit", "Glob", "Grep", "WebFetch", "WebSearch"],
        "deny": []
    }
}
```

3. **Environment script** — `/etc/profile.d/claude.sh`. Sets `CLAUDE_DANGEROUSLY_SKIP_PERMISSIONS=true` and any user-provided environment variables.

4. **Task metadata** — `/root/task/task.json`. The full task definition.

5. **Output directory marker** — `/root/output/.keep`. Creates the output directory so Claude Code has somewhere to write results.

6. **User-provided files** — Any files specified in the task request's `files` map.

### Step 6: Run Claude Code

The prompt is written to a temporary file inside the VM to avoid shell escaping issues, then referenced in the command:

```go
claudeArgs := fmt.Sprintf(
    "claude -p \"$(cat %s)\" --output-format stream-json --verbose",
    promptFile,
)

if t.MaxTurns > 0 {
    claudeArgs += fmt.Sprintf(" --max-turns %d", t.MaxTurns)
}

cmd := []string{"bash", "-c",
    "source /etc/profile.d/claude.sh && " + claudeArgs}
```

The command is executed via `vsock.ExecStream` which calls the agent's streaming exec. Each line of stdout/stderr is passed to the runner's `OnEvent` callback, which publishes it to the stream hub for WebSocket subscribers.

The full output is accumulated in a `strings.Builder` and stored on the task after completion.

### Step 7: Collect result files

Two searches inside the VM:

```go
// 1. Explicit output directory
vsock.Exec(jailID, []string{"find", t.OutputDir, "-type", "f", "-not", "-name", ".keep"}, nil, "/root")

// 2. Any new files under /root (created after the prompt file)
vsock.Exec(jailID, []string{"find", "/root", "-maxdepth", "2", "-type", "f",
    "-not", "-path", "/root/.claude/*",
    "-not", "-path", "/root/task/*",
    "-not", "-name", ".bashrc", "-not", "-name", ".profile",
    "-newer", "/tmp/claude-prompt.txt"}, nil, "/root")
```

Each file is downloaded via `vsock.ReadFile` and saved to `/opt/firecracker/results/<task-id>/<filename>`. Duplicate filenames get a `-2`, `-3` suffix.

### Cost parsing

The runner scans the accumulated output for a line containing `total_cost_usd` (from Claude's stream-json `result` event):

```go
func (r *Runner) parseCost(t *Task) {
    for _, line := range strings.Split(t.Output, "\n") {
        if strings.Contains(line, "total_cost_usd") {
            var result struct {
                TotalCostUSD float64 `json:"total_cost_usd"`
            }
            if json.Unmarshal([]byte(line), &result) == nil && result.TotalCostUSD > 0 {
                t.CostUSD = result.TotalCostUSD
            }
        }
    }
}
```

---

## 12. Credential Injection and Security

### Credential lifecycle

1. Host reads `~/.claude/.credentials.json` (OAuth tokens). If running under `sudo`, it resolves the real user's home directory via `SUDO_USER` environment variable.
2. Credentials are written to the VM via vsock with mode 0600 (only root can read).
3. Claude Code inside the VM uses these credentials to authenticate with the Anthropic API.
4. When the VM is destroyed, the rootfs (containing the credentials) is deleted.

### CLAUDE_DANGEROUSLY_SKIP_PERMISSIONS

This environment variable tells Claude Code to skip all tool permission checks. Normally Claude Code prompts the user before running shell commands or writing files. Inside a disposable VM with no valuable data, this is safe and necessary for autonomous operation.

### VM isolation

- KVM hardware virtualisation — separate kernel, separate memory space
- Jailer chroot — Firecracker process can't access the host filesystem
- Per-VM networking — separate subnet, no VM-to-VM routing
- Ephemeral — destroyed after task completion, no state persists

### What's NOT secured

- The MCP server on port 8081 has **no authentication**. Anyone on the local network can submit tasks and use your Claude API credits. This is acceptable for a home lab but not for production.
- The REST API on port 8080 also has no authentication.
- The guest MAC address is hardcoded (`06:00:AC:10:00:02`) for all VMs. This doesn't cause problems because VMs are on isolated subnets, but it's not ideal.

---

## 13. Output Streaming

### Stream Hub

`internal/stream/hub.go` — a per-task pub/sub system with a ring buffer.

The full data flow from Claude Code's stdout to the browser:

```
Claude Code stdout (in VM)
  → guest agent reads line via bufio.Scanner
  → agent sends StreamEvent frame over vsock
  → host vsock.ExecStream reads frame
  → task runner OnEvent callback fires
  → stream.Hub.Publish(event)
    → ring buffer (1000 events)
    → non-blocking fan-out to subscriber channels
  → WebSocket handler reads from channel
  → browser receives JSON message
  → React dashboard parses and renders
```

```go
type Hub struct {
    streams map[string]*Stream
}

type Stream struct {
    buffer      []agent.StreamEvent  // Ring buffer
    bufferSize  int                  // 1000
    bufferIdx   int
    bufferFull  bool
    subscribers map[chan agent.StreamEvent]struct{}
    closed      bool
}
```

Each task gets a `Stream` with a 1000-event ring buffer. Events are published from the task runner's `OnEvent` callback. When a WebSocket client connects, it receives:

1. All buffered history (up to 1000 events, in order)
2. Live events as they arrive

The fan-out is non-blocking — if a subscriber's channel is full, the event is dropped for that subscriber:

```go
for ch := range s.subscribers {
    select {
    case ch <- event:
    default:
        // Subscriber is slow, drop the event
    }
}
```

This prevents a slow WebSocket client from blocking the task runner.

### Data flow

```
Claude Code (in VM)
  → stdout line
  → agent StreamEvent frame (vsock)
  → vsock.ExecStream reads frame
  → runner.OnEvent callback
  → stream.Publish(event)
    → ring buffer
    → fan-out to WebSocket subscribers
  → accumulated in t.Output
```

---

## 14. REST API and WebSocket

`internal/api/server.go` + handler files. Uses chi router with middleware (logger, recoverer, request ID, CORS).

### Endpoints

```
GET    /api/v1/health                    → {"status": "ok"}
GET    /api/v1/stats                     → {total_vms, running_vms, total_tasks}

POST   /api/v1/vms                       → Create VM (201)
GET    /api/v1/vms                       → List VMs (200)
GET    /api/v1/vms/{name}                → Get VM (200)
DELETE /api/v1/vms/{name}                → Destroy VM (204)
POST   /api/v1/vms/{name}/stop           → Stop VM (200)
POST   /api/v1/vms/{name}/exec           → Exec command in VM (200)

POST   /api/v1/tasks                     → Create task (202 Accepted, runs async)
GET    /api/v1/tasks                     → List tasks (200)
GET    /api/v1/tasks/{id}                → Get task (200)
DELETE /api/v1/tasks/{id}                → Cancel task (204, destroys VM)
GET    /api/v1/tasks/{id}/stream         → WebSocket upgrade
GET    /api/v1/tasks/{id}/files          → List result files (200)
GET    /api/v1/tasks/{id}/files/{name}   → Download result file (200)

GET    /*                                → Embedded React SPA
```

### Task creation

`POST /api/v1/tasks` starts the task in a goroutine and immediately returns 202:

```go
go func() {
    s.taskRunner.OnEvent = func(id string, event agent.StreamEvent) {
        taskStream.Publish(event)
    }
    s.taskRunner.Run(context.Background(), t)
}()
writeJSON(w, http.StatusAccepted, t)
```

### WebSocket streaming

`GET /api/v1/tasks/{id}/stream` upgrades to WebSocket. The handler subscribes to the task's stream, replays history, then streams live events:

```go
stream := s.streamHub.GetOrCreate(id)
history, ch := stream.Subscribe()
defer stream.Unsubscribe(ch)

// Send buffered history
for _, event := range history {
    conn.Write(ctx, websocket.MessageText, marshalWSMessage(event))
}

// Stream new events
for event := range ch {
    conn.Write(ctx, websocket.MessageText, marshalWSMessage(event))
    if event.Type == "exit" {
        return
    }
}
```

WebSocket messages are JSON:
```json
{"type": "stdout", "data": "{\"type\":\"assistant\",\"message\":{...}}", "timestamp": "2026-03-23T11:20:10Z"}
```

---

## 15. Web Dashboard

React 18 + TypeScript + Tailwind CSS, built with Vite. Dark theme (gray-950 background, orange accents).

### Pages

**Dashboard** (`/`) — Stats cards (running VMs, total VMs, total tasks), a quick task input box, recent tasks list, running VMs list. Polls the API every 3 seconds.

**VMs** (`/vms`) — List of VMs with create form. Columns: name, state, RAM, vCPUs, guest IP, PID. Destroy button per VM.

**Tasks** (`/tasks`) — List of all tasks. Columns: ID, prompt, status, created time. Click to view detail.

**Task Detail** (`/tasks/:id`) — The most complex page. Shows:
- Status badge, prompt, VM name, exit code, cost, duration
- Result files with download links and image previews
- Parsed Claude Code output (see below)

### Stream-json parsing

Claude Code's `--output-format stream-json` outputs one JSON object per line. The dashboard parses these into human-readable blocks:

```typescript
interface ParsedEvent {
    type: 'thinking' | 'text' | 'tool_call' | 'tool_result' | 'system' | 'result' | 'raw';
    content: string;
    toolName?: string;
    toolInput?: string;
    isError?: boolean;
    cost?: number;
}
```

Parsing rules:
- `type: "system", subtype: "init"` → session info (model, version)
- `type: "assistant"` with `content` blocks → iterate blocks:
  - `block.type === "thinking"` → purple "Thinking" block
  - `block.type === "text"` → blue "Claude" block
  - `block.type === "tool_use"` → orange "Tool: <name>" block with input
- `type: "user"` with `tool_result` blocks → gray/red "Result" block (truncated to 2000 chars)
- `type: "result"` → green "Final Result" block with cost
- `type: "rate_limit_event"` → skipped
- Anything else → shown as raw monospace text

### WebSocket integration

The `useWebSocket` hook connects when the task is running and disconnects when it completes:

```typescript
const isRunning = task?.status === 'running' || task?.status === 'pending';
const { messages } = useWebSocket(isRunning ? `/api/v1/tasks/${taskId}/stream` : null);
```

The output panel auto-scrolls to the bottom as new events arrive. A green pulsing dot indicates live streaming.

### Image previews

Result files with image extensions (`.png`, `.jpg`, etc.) are shown inline as `<img>` tags pointing to the API file download endpoint. This means screenshots taken by Claude Code inside the VM are immediately visible in the dashboard.

---

## 16. MCP Server

The MCP (Model Context Protocol) server lets Claude Code instances delegate tasks to the orchestrator. Two transports are supported.

### Stdio transport (local)

```bash
sudo ./bin/orchestrator mcp
```

Used when Claude Code runs on the same machine. MCP messages flow over stdin/stdout. The logger is set to `Warn` level on stderr to avoid interfering with the MCP protocol on stdout.

Configure in Claude Code:
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

### Streamable HTTP transport (network)

```bash
sudo ./bin/orchestrator mcp-serve --addr 0.0.0.0:8081
```

Uses `mcp-go`'s `NewStreamableHTTPServer` with stateless mode. The endpoint is `/mcp`.

Configure in Claude Code on any machine on the network:
```json
{
    "mcpServers": {
        "orchestrator": {
            "type": "http",
            "url": "http://<your-server-ip>:8081/mcp"
        }
    }
}
```

### Registered tools

Eight tools, defined in `internal/mcp/server.go` and implemented in `internal/mcp/tools.go`:

**run_task** — The primary tool. Creates a VM, runs Claude Code, returns results. Blocks until complete.
- `prompt` (string, required)
- `ram_mb` (number, default 2048)
- `vcpus` (number, default 2)
- `timeout` (number, default 600 seconds)
- `max_turns` (number, default unlimited)
- `output_dir` (string, default `/root/output`)
- Returns: task_id, status, exit_code, result_files, cost_usd, duration_seconds, output (truncated to 4000 chars), hint

**get_task_status** — Returns the full task object as JSON.

**list_vms** — Returns a summary of all VMs (name, state, RAM, vCPUs, guest IP, PID).

**exec_in_vm** — Runs a shell command in a running VM. Used for interactive exploration if `auto_destroy` is false.

**read_vm_file** — Reads an arbitrary file from a running VM.

**destroy_vm** — Destroys a specific VM.

**list_task_files** — Lists result files with sizes and MIME types.

**get_task_file** — Returns file contents. The return type depends on the file:
- Text files (detected by MIME type or extension from a hardcoded list of ~30 extensions) → plain text
- Images → MCP image content (base64), so Claude can actually see screenshots
- Other binary → base64 JSON with metadata

```go
if isImageMime(mimeType) {
    encoded := base64.StdEncoding.EncodeToString(data)
    return mcplib.NewToolResultImage("Screenshot/image from task "+taskID, encoded, mimeType), nil
}
```

---

## 17. The SSE to Streamable HTTP Migration

This was a real issue encountered and fixed during development.

### The problem

The MCP server was initially built with `mcp-go v0.45.0` using the SSE (Server-Sent Events) transport:

```go
sseServer := server.NewSSEServer(s.mcpServer,
    server.WithBaseURL("http://"+addr),
)
sseServer.Start(addr)  // served on /sse
```

This worked until Claude Code updated to expect the newer **Streamable HTTP** transport. When connecting, Claude Code would attempt OAuth discovery against the `/sse` endpoint, receive a 404 (our server doesn't implement OAuth), and fail with:

```
Error: HTTP 404: Invalid OAuth error response: SyntaxError: JSON Parse error: Unable to parse JSON string
```

### The fix

1. Upgraded `mcp-go` from v0.45.0 to v0.46.0 (`go get github.com/mark3labs/mcp-go@latest`)
2. Replaced the SSE server with Streamable HTTP:

```go
// Before
func (s *Server) ServeSSE(addr string) error {
    sseServer := server.NewSSEServer(s.mcpServer,
        server.WithBaseURL("http://"+addr),
    )
    return sseServer.Start(addr)
}

// After
func (s *Server) ServeHTTP(addr string) error {
    httpServer := server.NewStreamableHTTPServer(s.mcpServer,
        server.WithEndpointPath("/mcp"),
        server.WithStateLess(true),
    )
    return httpServer.Start(addr)
}
```

3. Updated `cmd/orchestrator/main.go` to call the new method
4. Changed the Claude Code client config from the old SSE URL (type: sse) to `"url": "http://<your-server-ip>:8081/mcp"` (type: http)

The config lives in `~/.claude.json` under the project's `mcpServers` key. There's no `~/.claude/mcp.json` — Claude Code stores project-scoped MCP configs in the main config file.

---

## 18. CLI Interface

The binary supports five top-level commands:

### `orchestrator vm` — VM management

```bash
sudo ./bin/orchestrator vm create --name test1 --ram 2048 --vcpus 2
sudo ./bin/orchestrator vm list
sudo ./bin/orchestrator vm get test1
sudo ./bin/orchestrator vm stop test1
sudo ./bin/orchestrator vm destroy test1
```

All VM commands except `list` and `get` require root.

### `orchestrator task run` — One-shot task execution

```bash
sudo ./bin/orchestrator task run \
    --prompt "Write a Python script that prints hello world" \
    --ram 2048 \
    --vcpus 2 \
    --timeout 120 \
    --max-turns 10
```

Streams output directly to the terminal. Prints a summary on completion:

```
=== Task Complete ===
ID:     a3bfca80
Status: completed
Exit:   0
Cost:   $0.0582
Files:  [hello.py]
```

Add `--no-destroy` to keep the VM running after the task for inspection.

### `orchestrator serve` — API server

```bash
sudo ./bin/orchestrator serve --port 8080
```

Starts the REST API with embedded React dashboard. Open `http://localhost:8080` in a browser.

### `orchestrator mcp` — MCP over stdio

```bash
sudo ./bin/orchestrator mcp
```

For local Claude Code integration. MCP protocol over stdin/stdout.

### `orchestrator mcp-serve` — MCP over HTTP

```bash
sudo ./bin/orchestrator mcp-serve --addr 0.0.0.0:8081
```

For network Claude Code integration. Streamable HTTP on `/mcp`.

---

## 19. Known Limitations and Honest Trade-offs

### No persistence

The task store is an in-memory Go map. Orchestrator restart loses all task history. VM metadata already persists to disk and recovers on startup — tasks should too. SQLite or bbolt would require only a few hours of work; it just hasn't been needed due to infrequent restarts.

### No task queue or backpressure

Tasks fire as unbounded goroutines. Submitting 20 tasks on a 30GB machine where each VM needs 2GB causes later VMs to fail from lack of memory. A buffered channel or semaphore would solve this. Priority queues (quick code generation ahead of long research) could be added, but simple concurrency caps suffice initially.

### No authentication

The REST API and MCP server accept requests from anyone who can reach the port. Fine for a private LAN, not acceptable for anything internet-facing. For teams: API keys at minimum, mTLS for serious security. The MCP spec now supports auth flows — appropriate for the MCP endpoint.

### Double rootfs copy

Each VM creation copies the rootfs twice — once for network injection, once into the jailer chroot. This is wasteful and could be collapsed into a single copy with the network config injected directly into the chroot copy. It hasn't been optimised because boot time is dominated by Firecracker startup, not file copying (sparse copy of a 4GB image takes <1 second).

### Hardcoded paths

All paths (`/opt/firecracker/`, `/srv/jailer/`, etc.) are constants in the source code. There's no config file. To change them, you edit `internal/vm/config.go` and rebuild.

### Hardcoded MAC address

All VMs get the same guest MAC (`06:00:AC:10:00:02`). This works because each VM is on its own isolated subnet, but it would cause problems if you tried to bridge VMs onto the same network.

### OnEvent callback race

The task runner's `OnEvent` callback is set per-task but stored on the runner struct:

```go
s.taskRunner.OnEvent = func(id string, event agent.StreamEvent) {
    taskStream.Publish(event)
}
s.taskRunner.Run(context.Background(), t)
```

If two tasks are submitted simultaneously, they'll overwrite each other's callbacks. In practice this works because the MCP `run_task` tool blocks (only one MCP task at a time), and the API handler creates the stream before the goroutine runs. But it's a latent race condition that should be fixed by passing the callback into `Run()` directly.

### No graceful shutdown

There's no signal handler. `Ctrl-C` kills the process immediately. Running VMs are not cleaned up — they continue running as orphaned Firecracker processes. The `recoverState()` function on next startup will find them and track them again, but task data is lost. A proper signal handler would stop accepting new tasks, wait for running ones with a timeout, then clean shutdown.

### Cost parsing is best-effort

The cost is extracted by scanning the raw output for a line containing `total_cost_usd`. If Claude's output format changes or the line is missing, cost is reported as zero. It's not authoritative — check your Anthropic dashboard for real costs.

### No TLS

All communication (API, WebSocket, MCP) is unencrypted HTTP. Put it behind a reverse proxy with TLS for anything beyond a home lab.

### Sparse copy performance

`cp --sparse=always` is called via `exec.Command`, shelling out to the system `cp`. This works but is not the most efficient approach. A Go-native sparse copy using `SEEK_HOLE`/`SEEK_DATA` or reflinking (on supported filesystems) would be faster. In practice, the copy takes <1 second so this hasn't been worth optimising.

### Multi-user deployment gaps

For multi-user use, the system would need: S3/R2 result storage instead of local disk, a web auth layer, per-user credential vaults (to prevent token mixing between users), and usage tracking with cost attribution. None of this exists currently.

### What wouldn't change at scale

These design decisions remain sound regardless of scale:

- Single-binary deployment (`go:embed` for the frontend)
- Vsock for host-guest communication (no SSH, no shared mounts)
- Ephemeral VMs as the isolation model
- Embedded frontend

The architecture is sound — it's the operational bits around it that need work. Adding persistence, auth, and a task queue would bring the codebase from ~3,200 lines of Go to approximately 4,500. Still fits in your head.

---

## Appendix: File Inventory

```
cmd/
  orchestrator/
    main.go               # CLI entry point, all command handlers (340 lines)
    web-dist/             # Embedded frontend (built from web/)
  agent/
    main.go               # Guest agent (422 lines)

internal/
  agent/
    protocol.go           # Wire protocol types, ReadFrame/WriteFrame (163 lines)
    protocol_test.go      # Protocol tests
  api/
    server.go             # Router, middleware, SPA serving (156 lines)
    handlers_task.go      # Task CRUD + file serving (152 lines)
    handlers_vm.go        # VM CRUD + exec (handlers)
    handlers_ws.go        # WebSocket streaming (75 lines)
    types.go              # Request/response types (44 lines)
  inject/
    rootfs.go             # Mount rootfs, write files, network config (57 lines)
  mcp/
    server.go             # MCP server setup, tool registration (156 lines)
    tools.go              # Tool handler implementations (290 lines)
  network/
    addressing.go         # FNV hashing, IP/CID/TAP name allocation (56 lines)
    addressing_test.go    # Addressing tests
    tap.go                # TAP device create/destroy (65 lines)
    iptables.go           # NAT and FORWARD rule management (59 lines)
  stream/
    hub.go                # Pub/sub hub, ring buffer, fan-out (139 lines)
  task/
    task.go               # Task struct and defaults (59 lines)
    store.go              # In-memory task store (51 lines)
    runner.go             # Task execution pipeline (385 lines)
  vm/
    config.go             # Constants, VMConfig, VMInstance types (91 lines)
    manager.go            # VM lifecycle management (509 lines)
    metadata.go           # JSON persistence for VM state

web/
  src/
    App.tsx               # Navigation
    main.tsx              # Entry point
    types.ts              # TypeScript types
    hooks/
      useApi.ts           # Polling data fetcher
      useWebSocket.ts     # WebSocket hook (36 lines)
    pages/
      Dashboard.tsx       # Stats, quick task, recent tasks (128 lines)
      VMList.tsx           # VM list and create form
      TaskList.tsx         # Task list
      TaskDetail.tsx       # Stream parser, output viewer, file previews (356 lines)

docs/
  API_REFERENCE.md        # Full API docs
  MCP_SERVER.md           # MCP setup and usage
  TECHNICAL_DOCUMENTATION.md  # This file

Makefile                  # build, build-agent, all, test, clean
go.mod                    # 7 direct dependencies
```
