#!/usr/bin/env bash
# install-firecracker.sh — fetch Firecracker + jailer + a signed kernel and
# lay them out at ${ORCHESTRATOR_FC_BASE:-/opt/firecracker}.
#
# Usage: sudo scripts/install-firecracker.sh [version]
set -euo pipefail

VERSION="${1:-${ORCHESTRATOR_FC_VERSION:-v1.15.0}}"
FC_BASE="${ORCHESTRATOR_FC_BASE:-/opt/firecracker}"
ARCH="$(uname -m)"

case "$ARCH" in
	x86_64) FC_ARCH=x86_64 ;;
	aarch64) FC_ARCH=aarch64 ;;
	*) echo "ERROR: unsupported arch $ARCH" >&2; exit 1 ;;
esac

if [[ $EUID -ne 0 ]]; then
	echo "ERROR: must run as root (sudo $0)" >&2
	exit 1
fi

if [[ ! -e /dev/kvm ]]; then
	echo "ERROR: /dev/kvm not present — KVM support is required" >&2
	exit 1
fi

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

ASSET_URL="https://github.com/firecracker-microvm/firecracker/releases/download/${VERSION}/firecracker-${VERSION}-${FC_ARCH}.tgz"

echo "==> Downloading Firecracker $VERSION for $FC_ARCH"
curl -fsSL --retry 3 -o "$TMPDIR/fc.tgz" "$ASSET_URL"

echo "==> Extracting"
tar -xzf "$TMPDIR/fc.tgz" -C "$TMPDIR"

# The archive contains release-<ver>/<fc_arch>/firecracker-<ver>-<arch> and jailer-...
INNER="$TMPDIR/release-${VERSION}-${FC_ARCH}"
if [[ ! -d "$INNER" ]]; then
	INNER="$(find "$TMPDIR" -maxdepth 2 -type d -name 'release-*' | head -n1)"
fi

FC_BIN="$(find "$INNER" -name "firecracker-${VERSION}-${FC_ARCH}" | head -n1)"
JL_BIN="$(find "$INNER" -name "jailer-${VERSION}-${FC_ARCH}" | head -n1)"

if [[ -z "$FC_BIN" || -z "$JL_BIN" ]]; then
	echo "ERROR: could not locate firecracker/jailer binaries in archive" >&2
	exit 1
fi

echo "==> Installing to $FC_BASE"
mkdir -p "$FC_BASE/kernels" "$FC_BASE/rootfs" "$FC_BASE/vms" "$FC_BASE/results"
install -Dm755 "$FC_BIN" "$FC_BASE/firecracker"
install -Dm755 "$JL_BIN" "$FC_BASE/jailer"

# Download a guest kernel image if none is present.
KERNEL_URL_DEFAULT="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/x86_64/vmlinux-5.10.223"
KERNEL_URL="${ORCHESTRATOR_KERNEL_URL:-$KERNEL_URL_DEFAULT}"

if [[ ! -f "$FC_BASE/kernels/vmlinux" ]]; then
	echo "==> Downloading guest kernel ($KERNEL_URL)"
	curl -fsSL --retry 3 -o "$FC_BASE/kernels/vmlinux" "$KERNEL_URL"
fi

echo ""
echo "Firecracker installed:"
echo "  $FC_BASE/firecracker      $("$FC_BASE/firecracker" --version 2>&1 | head -n1)"
echo "  $FC_BASE/jailer           $("$FC_BASE/jailer" --version 2>&1 | head -n1)"
echo "  $FC_BASE/kernels/vmlinux  $(du -h "$FC_BASE/kernels/vmlinux" | cut -f1)"
echo ""
echo "Next: build the guest rootfs"
echo "  make build-agent"
echo "  sudo make rootfs"
