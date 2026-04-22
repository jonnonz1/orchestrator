# Installation

## Prerequisites

Orchestrator runs on Linux with KVM enabled. Verified on Ubuntu 24.04; should
work on any distribution that ships a recent kernel and systemd.

| Requirement | Why |
|---|---|
| Linux x86_64 | Firecracker is Linux-only and only ships amd64 + arm64 binaries. |
| KVM enabled | `ls /dev/kvm` should succeed. `kvm-ok` is a friendly check. |
| Root / passwordless sudo | VM and iptables operations require root. |
| Go ≥ 1.26.2 | Host binary build. |
| Node.js ≥ 20 | Frontend build (embedded dashboard). |
| 4 GB+ free RAM | Each task VM defaults to 2 GB. |
| 20 GB+ free disk | Base rootfs + per-VM copies. |

Check KVM is actually usable:

```bash
[ -r /dev/kvm ] && [ -w /dev/kvm ] && echo "KVM OK" || echo "KVM unavailable"
```

If the guest kernel should live elsewhere, adjust `ORCHESTRATOR_FC_BASE` before
running `make install-firecracker`. Defaults assume `/opt/firecracker`.

## Install Firecracker + jailer + guest kernel

```bash
sudo make install-firecracker
```

This script fetches the pinned Firecracker v1.15.0 release, installs
`firecracker` and `jailer` binaries under `/opt/firecracker/`, and downloads
the matching guest kernel to `/opt/firecracker/kernels/vmlinux`.

If you prefer to install these manually, replicate what
`scripts/install-firecracker.sh` does and verify `/opt/firecracker/firecracker
--version` returns v1.15.0.

## Build the host binary and guest agent

```bash
make build           # builds bin/orchestrator with embedded dashboard
make build-agent     # builds bin/agent (static linux/amd64) for the guest
```

`make build` pulls the frontend deps, runs `vite build`, copies the output to
`cmd/orchestrator/web-dist/`, and links it into the Go binary via `go:embed`.

## Build the guest rootfs

```bash
sudo make rootfs
```

First run takes ~10 minutes. It builds a Debian Bookworm rootfs with Node 24,
npm, Chromium, Python 3.11, git, and Claude Code pre-installed, then embeds
the guest agent binary at `/usr/local/bin/agent` with a systemd service. The
result is cached at `/opt/firecracker/rootfs/base-rootfs.ext4` (~4 GB).

Subsequent VM creation is a sparse copy of this template — that's why cold
boot is 4 seconds.

## Verify

```bash
# Sanity check: list running VMs (none yet)
sudo ./bin/orchestrator vm list

# Create one, get its metadata, destroy it
sudo ./bin/orchestrator vm create --name smoke --ram 1024 --vcpus 2
sudo ./bin/orchestrator vm get  --name smoke
sudo ./bin/orchestrator vm destroy --name smoke
```

If `vm create` succeeds and `vm destroy` cleans everything up, your setup is
good. Move on to the [quickstart](quickstart.md).

## Troubleshooting install

| Symptom | Fix |
|---|---|
| `permission denied on /dev/kvm` | Add your user to the `kvm` group or run everything as root. |
| `KVM_CREATE_VM failed` | Nested virtualisation off on a cloud VM — enable in the hypervisor. |
| `iptables: Permission denied` | You're not root. VM ops need root for TAP + iptables. |
| `rootfs build fails on network` | Check UFW / outbound HTTPS. The build pulls packages from Debian and npm. |
| `jailer: Permission denied` | `/opt/firecracker` ownership got clobbered; `sudo chown -R root:root /opt/firecracker`. |

More troubleshooting lives in the [troubleshooting guide](../guides/troubleshooting.md).
