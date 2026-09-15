#!/usr/bin/env bash
# Build the two assets a microVM needs: a kernel (vmlinux) and a base rootfs
# (base.ext4) with the anviq guest agent baked in as PID 1. Output lands in
# $ASSET_DIR (default /opt/anviq), exactly where fcctl expects it.
#
# This is the step that turns "I have a KVM box" into "make smoke boots a VM".
# It is intentionally dependency-light: root, mkfs.ext4, curl (or wget), tar.
#
# Everything is overridable by env var so you can pin your own kernel/rootfs
# sources — no hidden downloads baked into the control plane.
set -euo pipefail

ASSET_DIR="${ASSET_DIR:-/opt/anviq}"
ROOTFS_MB="${ROOTFS_MB:-512}"
ARCH="${ARCH:-$(uname -m)}"

# Alpine minirootfs: tiny, stable, static-friendly. Override to use your own base.
ALPINE_VER="${ALPINE_VER:-3.20}"
ALPINE_PATCH="${ALPINE_PATCH:-3.20.3}"
ALPINE_ARCH="${ALPINE_ARCH:-$ARCH}"
ROOTFS_URL="${ROOTFS_URL:-https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VER}/releases/${ALPINE_ARCH}/alpine-minirootfs-${ALPINE_PATCH}-${ALPINE_ARCH}.tar.gz}"

# Kernel: a Firecracker-compatible uncompressed vmlinux with virtio-vsock enabled.
# There is no single stable public URL for this across versions, so KERNEL_URL has
# no default — set it to a vmlinux you trust, or drop one at $ASSET_DIR/vmlinux
# yourself and this script will keep it.
KERNEL_URL="${KERNEL_URL:-}"

log() { printf '\033[1;36m[build-rootfs]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[build-rootfs] %s\033[0m\n' "$*" >&2; exit 1; }
fetch() { if command -v curl >/dev/null; then curl -fsSL "$1" -o "$2"; else wget -qO "$2" "$1"; fi; }

[ "$(id -u)" -eq 0 ] || die "must run as root (loop mount + chroot)"
command -v mkfs.ext4 >/dev/null || die "mkfs.ext4 not found (install e2fsprogs)"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
mkdir -p "$ASSET_DIR"

# 1. Build the static guest agent for the target arch.
log "building static guest agent"
( cd "$REPO_ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH:-amd64}" \
    go build -o "$ASSET_DIR/anviq-guest" ./cmd/anviq-guest )

# 2. Kernel.
if [ -f "$ASSET_DIR/vmlinux" ]; then
  log "kernel already present at $ASSET_DIR/vmlinux (kept)"
elif [ -n "$KERNEL_URL" ]; then
  log "downloading kernel"
  fetch "$KERNEL_URL" "$ASSET_DIR/vmlinux"
else
  cat >&2 <<EOF
[build-rootfs] No kernel and no KERNEL_URL set.
  Provide an uncompressed, Firecracker-compatible vmlinux with virtio-vsock enabled:
    - set KERNEL_URL=... and re-run, or
    - drop your vmlinux at $ASSET_DIR/vmlinux and re-run.
  (The Firecracker guest kernel config enables CONFIG_VIRTIO_VSOCKETS — the agent
   needs AF_VSOCK in the guest.)
EOF
  die "kernel required"
fi

# 3. Rootfs image.
IMG="$ASSET_DIR/base.ext4"
MNT="$(mktemp -d)"
cleanup() { umount "$MNT" 2>/dev/null || true; rmdir "$MNT" 2>/dev/null || true; }
trap cleanup EXIT

log "creating ${ROOTFS_MB}MB ext4 image"
rm -f "$IMG"
dd if=/dev/zero of="$IMG" bs=1M count="$ROOTFS_MB" status=none
mkfs.ext4 -q "$IMG"
mount -o loop "$IMG" "$MNT"

log "unpacking base userland"
TARBALL="$(mktemp)"
fetch "$ROOTFS_URL" "$TARBALL"
tar -xzf "$TARBALL" -C "$MNT"
rm -f "$TARBALL"

log "installing guest agent + init"
install -Dm755 "$ASSET_DIR/anviq-guest" "$MNT/usr/local/bin/anviq-guest"

# PID 1: mount the essentials the agent relies on, then hand off to the agent.
# vsock needs no networking, so this stays minimal on purpose.
cat > "$MNT/sbin/init" <<'INIT'
#!/bin/sh
mount -t proc  proc  /proc  2>/dev/null
mount -t sysfs sys   /sys   2>/dev/null
mount -t devtmpfs dev /dev  2>/dev/null
# Ensure the guest vsock transport is available (built-in on the Firecracker kernel;
# these modprobes are harmless no-ops if compiled in).
modprobe vsock 2>/dev/null || true
modprobe vmw_vsock_virtio_transport 2>/dev/null || true
exec /usr/local/bin/anviq-guest
INIT
chmod 755 "$MNT/sbin/init"

sync
log "done: $ASSET_DIR/vmlinux  +  $IMG"
log "fcctl boots this rootfs read-only per VM via a copy-on-write overlay."
