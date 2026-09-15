#!/usr/bin/env bash
# One-time host prep for Plan A. Brings a KVM-capable Linux box from bare to ready:
# verifies virtualization, creates the bridge + NAT, and stages the kernel + base rootfs.
#
# Idempotent: safe to re-run. Requires root.
set -euo pipefail

BRIDGE="${BRIDGE:-br0}"
SUBNET="${SUBNET:-192.168.100.1/24}"
UPLINK="${UPLINK:-$(ip route show default | awk '/default/ {print $5; exit}')}"
ASSET_DIR="${ASSET_DIR:-/opt/anviq}"
STATE_DIR="${STATE_DIR:-/var/lib/anviq}"

log() { printf '\033[1;32m[host-setup]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[host-setup] %s\033[0m\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "must run as root"

# 1. Virtualization must be available — this is the whole premise.
[ -e /dev/kvm ] || die "/dev/kvm not present: enable virtualization (nested virt if this is a cloud VM)"
log "KVM present"

# 2. Bridge for VM TAP devices.
if ! ip link show "$BRIDGE" >/dev/null 2>&1; then
  log "creating bridge $BRIDGE ($SUBNET)"
  ip link add "$BRIDGE" type bridge
  ip addr add "$SUBNET" dev "$BRIDGE"
  ip link set "$BRIDGE" up
else
  log "bridge $BRIDGE already exists"
fi

# 3. NAT so microVMs reach the uplink (no inbound; egress only).
[ -n "$UPLINK" ] || die "could not determine uplink interface; set UPLINK=..."
sysctl -qw net.ipv4.ip_forward=1
if ! iptables -t nat -C POSTROUTING -o "$UPLINK" -j MASQUERADE 2>/dev/null; then
  log "adding NAT masquerade via $UPLINK"
  iptables -t nat -A POSTROUTING -o "$UPLINK" -j MASQUERADE
  iptables -A FORWARD -i "$BRIDGE" -o "$UPLINK" -j ACCEPT
  iptables -A FORWARD -i "$UPLINK" -o "$BRIDGE" -m state --state RELATED,ESTABLISHED -j ACCEPT
fi

# 4. Directories.
mkdir -p "$ASSET_DIR" "$STATE_DIR"

# 5. Kernel + base rootfs.
#    Firecracker needs an uncompressed kernel (vmlinux) and a RAW ext4 rootfs with the
#    anviq guest agent installed as init. scripts/build-rootfs.sh produces both.
if [ ! -f "$ASSET_DIR/vmlinux" ] || [ ! -f "$ASSET_DIR/base.ext4" ]; then
  cat <<EOF
[host-setup] Kernel and/or base rootfs not yet built in $ASSET_DIR.
  Build them:  sudo KERNEL_URL=<your vmlinux url> ./scripts/build-rootfs.sh   (or make rootfs)
  (Or drop your own $ASSET_DIR/vmlinux and $ASSET_DIR/base.ext4 in place.)
EOF
else
  log "assets present: vmlinux + base.ext4"
fi

log "host ready: bridge=$BRIDGE uplink=$UPLINK assets=$ASSET_DIR state=$STATE_DIR"
log "next: make rootfs (if needed) && make build && sudo ANVIQ_CONTROL_TOKEN=... ./bin/fcctl && make smoke"
