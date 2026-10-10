#!/usr/bin/env bash
set -euo pipefail
sudo systemctl disable --now 115strm 2>/dev/null || true
sudo rm -f /etc/systemd/system/115strm.service
sudo systemctl daemon-reload
echo "Data kept at /var/lib/115strm, logs at /var/log/115strm, media at /opt/Media"
