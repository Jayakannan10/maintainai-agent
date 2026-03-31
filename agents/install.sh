#!/usr/bin/env bash
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "Please run as root (sudo)." >&2
  exit 1
fi

# Production host for fetching artifacts and deriving webhook URL.
BASE_URL="https://automated-maintenance-ai.vercel.app"

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) BIN="maintainai-agent-linux-amd64" ;;
  aarch64|arm64) BIN="maintainai-agent-linux-arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 3
    ;;
esac

AGENT_URL="${BASE_URL%/}/agents/${BIN}"
CFG_URL="${BASE_URL%/}/agents/maintainai-agent.example.toml"
ENV_URL="${BASE_URL%/}/agents/maintainai-agent.example.env"
WEBHOOK_URL="${BASE_URL%/}/api/webhook/logs"

echo "Downloading agent: $AGENT_URL"
curl -fsSL "$AGENT_URL" -o /usr/local/bin/maintainai-agent
chmod 0755 /usr/local/bin/maintainai-agent

if [[ ! -f /etc/maintainai-agent.toml ]]; then
  echo "Downloading config template: $CFG_URL"
  curl -fsSL "$CFG_URL" -o /etc/maintainai-agent.toml
  # Auto-populate webhook.url from BASE_URL so only token needs editing.
  sed -i.bak "s|\${MAINTAINAI_WEBHOOK_URL}|${WEBHOOK_URL}|g" /etc/maintainai-agent.toml
  rm -f /etc/maintainai-agent.toml.bak
  chmod 0644 /etc/maintainai-agent.toml
  echo "Created /etc/maintainai-agent.toml (edit webhook.token, logs.paths)."
else
  echo "Keeping existing /etc/maintainai-agent.toml"
fi

if [[ ! -f /etc/maintainai-agent.env ]]; then
  echo "Downloading env template: $ENV_URL"
  curl -fsSL "$ENV_URL" -o /etc/maintainai-agent.env
  chmod 0600 /etc/maintainai-agent.env
  echo "Created /etc/maintainai-agent.env (set MAINTAINAI_WEBHOOK_URL, ACCESS_TOKEN)."
else
  echo "Keeping existing /etc/maintainai-agent.env"
fi

cat >/etc/systemd/system/maintainai-agent.service <<'EOF'
[Unit]
Description=MaintainAI Server Logs Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root
EnvironmentFile=-/etc/maintainai-agent.env
ExecStart=/usr/local/bin/maintainai-agent --config /etc/maintainai-agent.toml
Restart=always
RestartSec=2
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=true
ReadWritePaths=/var/lib/maintainai-agent /var/log

[Install]
WantedBy=multi-user.target
EOF

mkdir -p /var/lib/maintainai-agent
chmod 0755 /var/lib/maintainai-agent

systemctl daemon-reload
systemctl enable maintainai-agent
systemctl restart maintainai-agent

echo "Installed."
echo "Check: systemctl status maintainai-agent"

