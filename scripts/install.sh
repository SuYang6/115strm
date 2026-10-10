#!/usr/bin/env bash
set -euo pipefail

sudo useradd -r -s /usr/sbin/nologin 115strm 2>/dev/null || true
sudo mkdir -p /opt/115strm/bin /etc/115strm /var/lib/115strm /var/log/115strm /opt/Media
sudo cp ./bin/115strm /opt/115strm/bin/115strm
if [ ! -f /etc/115strm/config.yaml ]; then
  sudo cp ./configs/config.yaml.example /etc/115strm/config.yaml
fi
sudo cp ./deploy/115strm.service /etc/systemd/system/115strm.service
sudo chown -R 115strm:115strm /opt/115strm /var/lib/115strm /var/log/115strm /opt/Media
sudo chown 115strm:115strm /etc/115strm/config.yaml
sudo chmod 600 /etc/115strm/config.yaml
sudo systemctl daemon-reload
sudo systemctl enable --now 115strm
