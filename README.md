## MaintainAI Agent (Linux server logs)

This repo contains the **client-side daemon** that tails log files on a Linux server and sends warning/error events to MaintainAI.

### What it sends
- `POST {webhook.url}`
- Header: `Authorization: Bearer {webhook.token}`
- Body: `{ type:"server_log", message, level, source, metadata }`

### Build (creates binaries)
```bash
bash scripts/build.sh
```

Outputs:
- `dist/maintainai-agent-linux-amd64`
- `dist/maintainai-agent-linux-arm64`

### Install scripts
`install.sh` / `uninstall.sh` expect that binaries + the TOML template are hosted at:
- `{BASE_URL}/agents/maintainai-agent-linux-amd64`
- `{BASE_URL}/agents/maintainai-agent-linux-arm64`
- `{BASE_URL}/agents/maintainai-agent.example.toml`

