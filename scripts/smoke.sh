#!/usr/bin/env bash
# Phase 1 exit criterion, executable. Boots a microVM through the running control plane,
# runs a command inside it, and destroys it — cleanly, so nothing leaks on the host.
#
# Assumes fcctl is already running and reachable, and ANVIQ_CONTROL_TOKEN is exported.
set -euo pipefail

BASE="${ANVIQ_BASE:-http://127.0.0.1:8080}"
TOKEN="${ANVIQ_CONTROL_TOKEN:?export the same token fcctl was started with}"
auth=(-H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json")

say() { printf '\033[1;36m[smoke]\033[0m %s\n' "$*"; }

say "creating microVM"
id=$(curl -fsS "${auth[@]}" -X POST "$BASE/v1/sandboxes" \
  -d '{"team_id":"smoke","vcpus":2,"mem_mib":2048}' | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
[ -n "$id" ] || { echo "no id returned"; exit 1; }
say "booted $id"

say "running: uname -a"
curl -fsS "${auth[@]}" -X POST "$BASE/v1/sandboxes/$id/exec" -d '{"argv":["uname","-a"]}'

say "destroying $id"
curl -fsS "${auth[@]}" -X DELETE "$BASE/v1/sandboxes/$id"

say "done — verify no leftover: ip link | grep tap- ; ls \$STATE_DIR"
