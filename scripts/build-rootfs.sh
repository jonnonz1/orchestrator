#!/usr/bin/env bash
# build-rootfs.sh — create the base guest rootfs for Orchestrator MicroVMs.
#
# Produces a Debian Bookworm ext4 image with:
#   - Node.js + npm
#   - Anthropic's Claude Code CLI
#   - Chromium (for headless browser tasks)
#   - Python 3
#   - git, curl, ca-certificates
#   - The Orchestrator guest agent installed as a systemd service
#
# Requires: debootstrap, mkfs.ext4, chroot, sudo.
# Output: ${ORCHESTRATOR_FC_BASE:-/opt/firecracker}/rootfs/base-rootfs.ext4
#
# This script is deliberately pinned to specific package versions for
# reproducibility. Bump the versions below in one place to roll the image.
set -euo pipefail

# ---- Configuration -------------------------------------------------------

FC_BASE="${ORCHESTRATOR_FC_BASE:-/opt/firecracker}"
ROOTFS_OUT="${FC_BASE}/rootfs/base-rootfs.ext4"
ROOTFS_SIZE_MB="${ORCHESTRATOR_ROOTFS_SIZE_MB:-4096}"
SUITE="${ORCHESTRATOR_DEBIAN_SUITE:-bookworm}"
MIRROR="${ORCHESTRATOR_DEBIAN_MIRROR:-http://deb.debian.org/debian}"

NODE_VERSION="${ORCHESTRATOR_NODE_VERSION:-24}"
CLAUDE_VERSION="${ORCHESTRATOR_CLAUDE_VERSION:-}" # empty = latest
AGENT_BIN="${ORCHESTRATOR_AGENT_BIN:-$(pwd)/bin/agent}"

# ---- Pre-flight ----------------------------------------------------------

if [[ $EUID -ne 0 ]]; then
	echo "ERROR: must run as root (sudo $0)" >&2
	exit 1
fi

for cmd in debootstrap mkfs.ext4 chroot mount umount; do
	if ! command -v "$cmd" >/dev/null 2>&1; then
		echo "ERROR: missing command: $cmd" >&2
		echo "Install with: apt-get install debootstrap e2fsprogs util-linux" >&2
		exit 1
	fi
done

if [[ ! -x "$AGENT_BIN" ]]; then
	echo "ERROR: guest agent binary not found at $AGENT_BIN" >&2
	echo "Build it first: make build-agent" >&2
	exit 1
fi

mkdir -p "$(dirname "$ROOTFS_OUT")"

# ---- Create empty ext4 image --------------------------------------------

echo "==> Creating ${ROOTFS_SIZE_MB}MB ext4 image at $ROOTFS_OUT"
truncate -s "${ROOTFS_SIZE_MB}M" "$ROOTFS_OUT"
mkfs.ext4 -F -q -L orch-rootfs "$ROOTFS_OUT"

MOUNT_DIR="$(mktemp -d)"
trap 'set +e; umount -R "$MOUNT_DIR" 2>/dev/null; rmdir "$MOUNT_DIR" 2>/dev/null' EXIT
mount -o loop "$ROOTFS_OUT" "$MOUNT_DIR"

# ---- Debootstrap base system --------------------------------------------

echo "==> Bootstrapping Debian $SUITE"
debootstrap --variant=minbase --arch=amd64 "$SUITE" "$MOUNT_DIR" "$MIRROR"

# Bind-mount for chroot operations.
mount -t proc proc "$MOUNT_DIR/proc"
mount -t sysfs sys "$MOUNT_DIR/sys"
mount -o bind /dev "$MOUNT_DIR/dev"
mount -o bind /dev/pts "$MOUNT_DIR/dev/pts"

# ---- Install packages inside the chroot ---------------------------------

echo "==> Installing base packages"
chroot "$MOUNT_DIR" bash -c '
	set -euo pipefail
	export DEBIAN_FRONTEND=noninteractive
	apt-get update -qq
	apt-get install -y --no-install-recommends \
		systemd systemd-sysv dbus \
		ca-certificates curl wget \
		git openssh-server \
		python3 python3-pip \
		chromium \
		fonts-liberation \
		iproute2 iputils-ping \
		vim-tiny less
	apt-get clean
	rm -rf /var/lib/apt/lists/*
'

echo "==> Installing Node.js $NODE_VERSION"
chroot "$MOUNT_DIR" bash -c "
	set -euo pipefail
	curl -fsSL https://deb.nodesource.com/setup_${NODE_VERSION}.x | bash -
	apt-get install -y --no-install-recommends nodejs
	apt-get clean
	rm -rf /var/lib/apt/lists/*
"

echo "==> Installing Claude Code CLI ${CLAUDE_VERSION:-latest}"
chroot "$MOUNT_DIR" bash -c "
	set -euo pipefail
	if [[ -n '${CLAUDE_VERSION}' ]]; then
		npm install -g @anthropic-ai/claude-code@${CLAUDE_VERSION}
	else
		npm install -g @anthropic-ai/claude-code
	fi
"

# ---- Install the Orchestrator guest agent --------------------------------

echo "==> Installing Orchestrator guest agent"
install -Dm755 "$AGENT_BIN" "$MOUNT_DIR/usr/local/bin/agent"

cat >"$MOUNT_DIR/etc/systemd/system/orchestrator-agent.service" <<'EOF'
[Unit]
Description=Orchestrator Guest Agent (vsock)
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/agent
Restart=always
RestartSec=1

[Install]
WantedBy=multi-user.target
EOF

chroot "$MOUNT_DIR" systemctl enable orchestrator-agent.service

# ---- Guest configuration ------------------------------------------------

echo "==> Configuring guest"

# Root password (for emergency console only; SSH is disabled by default).
echo "root:orchestrator" | chroot "$MOUNT_DIR" chpasswd

# Ensure /root/output exists for the result collection pipeline.
install -d -m0755 "$MOUNT_DIR/root/output"

# Shrink initrd setup — we boot with -kernel directly, no initrd.
cat >"$MOUNT_DIR/etc/fstab" <<'EOF'
/dev/vda / ext4 defaults 0 1
proc     /proc proc defaults 0 0
sysfs    /sys sysfs defaults 0 0
EOF

# Disable services we don't need inside a single-run VM.
chroot "$MOUNT_DIR" systemctl disable ssh 2>/dev/null || true
chroot "$MOUNT_DIR" systemctl mask apt-daily.service apt-daily.timer \
	apt-daily-upgrade.service apt-daily-upgrade.timer 2>/dev/null || true

# ---- Done ---------------------------------------------------------------

echo "==> Unmounting"
umount -R "$MOUNT_DIR"
rmdir "$MOUNT_DIR"
trap - EXIT

echo ""
echo "Rootfs built: $ROOTFS_OUT ($(du -h "$ROOTFS_OUT" | cut -f1))"
echo "Launch a VM: sudo orchestrator vm create --name test --ram 1024 --vcpus 2"
