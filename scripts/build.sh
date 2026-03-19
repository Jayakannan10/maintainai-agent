#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT_DIR/dist"

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required to build the agent." >&2
  echo "Install Go (1.22+) and re-run this script." >&2
  exit 2
fi

mkdir -p "$OUT_DIR"

echo "Building maintainai-agent (linux/amd64)..."
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$OUT_DIR/maintainai-agent-linux-amd64" ./cmd/maintainai-agent

echo "Building maintainai-agent (linux/arm64)..."
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o "$OUT_DIR/maintainai-agent-linux-arm64" ./cmd/maintainai-agent

chmod 0755 "$OUT_DIR/maintainai-agent-linux-amd64" "$OUT_DIR/maintainai-agent-linux-arm64"

echo "Done. Binaries are in: $OUT_DIR"

