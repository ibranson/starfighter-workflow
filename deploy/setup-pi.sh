#!/usr/bin/env bash
# deploy/setup-pi.sh — one-time install of the sfworkflowd systemd service.
#
# Idempotent: safe to re-run. Run from the directory that holds this script
# and sfworkflowd.service (provision.ps1 scps both to /tmp/sfworkflowd-setup
# and runs this under sudo).
#
# Deliberately minimal. Unlike the A/V appliance setup, this installs NO mpv,
# NO ffmpeg, NO getty masking, NO hotspot — it's a pure network service that
# assumes the Pi already has working networking (it coexists with the A/V app
# that brought the box online). It does not touch tty1, HDMI, or the A/V
# daemon in any way.
set -euo pipefail

UNIT=sfworkflowd.service
SRC_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "[setup] installing $UNIT"
install -m 0644 "$SRC_DIR/$UNIT" "/etc/systemd/system/$UNIT"

# Ensure the config directory exists (systemd's ConfigurationDirectory also
# creates it, but provision.ps1 writes the config before the first start).
install -d -m 0755 /etc/sfworkflowd

echo "[setup] reloading systemd + enabling $UNIT"
systemctl daemon-reload
systemctl enable "$UNIT"

echo "[setup] done. Push the binary with scripts/deploy.ps1, which restarts the service."
