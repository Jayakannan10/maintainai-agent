#!/usr/bin/env bash
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "Please run as root (sudo)." >&2
  exit 1
fi

systemctl stop maintainai-agent 2>/dev/null || true
systemctl disable maintainai-agent 2>/dev/null || true

rm -f /etc/systemd/system/maintainai-agent.service
systemctl daemon-reload || true

rm -f /usr/local/bin/maintainai-agent

echo "Uninstalled binary + service."
echo "Keeping config/state by default:"
echo "  /etc/maintainai-agent.toml"
echo "  /etc/maintainai-agent.env"
echo "  /var/lib/maintainai-agent/"
echo ""
echo "If you want to remove them too, run:"
echo "  sudo rm -f /etc/maintainai-agent.toml"
echo "  sudo rm -f /etc/maintainai-agent.env"
echo "  sudo rm -rf /var/lib/maintainai-agent"

