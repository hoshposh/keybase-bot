# SimpleX to Obsidian Bot

A lightweight Go headless service to bridge messages from SimpleX Chat and Feedly to a local Obsidian vault using the Model Context Protocol (MCP).

## Features

- **MCP Integration**: Uses the Model Context Protocol (`mcpvault`) to safely append notes to your Obsidian vault without manual format hacking.
- **Dual-Input Gateway**:
    - **SimpleX Chat**: Securely receive messages from a trusted SimpleX connection (e.g. from your Android device).
    - **Feedly Webhooks**: Save articles to your Obsidian vault from Feedly.
- **Prefix Routing (via SimpleX)**:
    - `!note [text]` appends to `[VaultPath]/Inbox.md`
    - `!todo [text]` appends to `[VaultPath]/Tasks.md`
    - `!link [url]` appends to `[VaultPath]/Links.md`
    - Any message starting with `http://` or `https://` (and containing no spaces) also appends to `[VaultPath]/Links.md`.
    - No prefix appends to `[VaultPath]/Daily/YYYY-MM-DD.md`
- **Google Drive Sync**: Automatically syncs your `/Research` directory to Google Drive (for NotebookLM grounding) securely via `rclone`.

## Deployment Architecture (Roles)

The bot can be executed in three different deployment modes via the `-role` flag, allowing you to establish a secure, remote-friendly architecture:

1. **Standalone** (`-role=standalone`): The default mode. Runs all features (Webhooks, SimpleX Listener, MCP, and Sync) in a single local process.
2. **Cloud Ingestor** (`-role=ingestor`): A secure gateway designed to run on a cloud server. It exposes the webhook HTTP server and establishes a lightweight SimpleX contact link to deliver payloads to the Executor. It does not perform any local MCP or Vault commands.
3. **Local Executor** (`-role=executor`): Runs locally behind your firewall. It hosts a long-term SimpleX address and listens for incoming payloads sent by the Ingestor or your mobile device, safely executing those payloads into your local Obsidian Vault using MCP.

By splitting the architecture, your Vault remains entirely air-gapped from the public Internet while you can still host the webhook ingestor anywhere.

## SimpleX Setup Guide

The bot relies on the `simplex-chat` CLI to orchestrate decentralized, end-to-end encrypted message queues. Since there is no central server, the local Executor acts as a target node by hosting a long-term connection address, which your Android device or Cloud Ingestors can connect to.

### 1. Install SimpleX CLI

You must have `simplex-chat` accessible in your PATH.
```bash
curl -o- https://raw.githubusercontent.com/simplex-chat/simplex-chat/stable/install.sh | bash
```

### 2. Auto-Provisioning Supported

You do not need to manually configure the profiles. The umbilical wizard will automatically spin up `simplex-chat` in a background WebSocket mode to negotiate the profile creation and configure your connection strings.

Run the wizard to provision your nodes:
```bash
./umbilical -setup
```

If you specify the `-role=executor`, the wizard will output a permanent **SimpleX Contact Address** (e.g. `smp://...`).
You will then provide this address to your Android SimpleX App to pair with it, and also configure the Cloud Ingestor to connect to it.

> [!NOTE]
> The Cloud Ingestor is designed to be lightweight. Each webhook instance spins up a lightweight SimpleX link request to the Executor, drops the payload, and closes, ensuring no long-running session state is needed on the cloud server.


## Prerequisites

1. **MCP Server**: You must have `mcpvault` installed globally or available in the path:
   ```bash
   npm install -g @bitbonsai/mcpvault
   ```
2. **Rclone** (Optional): For the Google Drive sync feature, you must have `rclone` installed and configured with a remote (e.g., `gdrive:`).

## Building

This project uses [Task](https://taskfile.dev/) as a task runner. To build a lightweight binary, run:

```bash
task build
```

Other available tasks:
- `task test`: Runs the unit test suite.
- `task clean`: Removes the generated binary.

## Running

You will need the following information to run the bot:
- `-role`: (Optional) The deployment role. One of `standalone`, `ingestor`, or `executor`. Default is `standalone`.
- `-executor-address`: (Required for `ingestor`) The SimpleX long-term address of your Executor node.
- `-vault`: (Required for `standalone`/`executor`) The absolute path to your Obsidian vault.
- `-bot-profile`: The name of the SimpleX profile folder (e.g., `mybot`).
- `-simplex-port`: The local port to run the SimpleX WebSocket server on. Default is `5225`.
- `-allowed-sender`: (Required for `standalone`/`ingestor`) The SimpleX contact alias allowed to send commands to the bot.
- `-mcp-cmd`: (Optional) The command to run the MCP server. Default is `npx -y @bitbonsai/mcpvault`.
- `-webhook-port`: (Optional) Port for the Feedly webhook HTTP server. Default is `8080`.
- `-webhook-secret`: (Optional) A shared secret token to expect in the `Authorization: Bearer <secret>` header from Feedly.
- `-sync-remote`: (Optional) If provided, initiates a background rclone sync loop. Example: `gdrive:ObsidianResearch`.
- `-sync-interval`: (Optional) Duration between syncs. Default is `15m`.

You can easily step through a configuration wizard that will generate your `config.json` by running:

```bash
./umbilical -setup
```

Or you can run the bot bypassing the wizard entirely via CLI arguments:

```bash
./umbilical \
  -role="standalone" \
  -vault="/path/to/vault" \
  -bot-profile="mybot" \
  -allowed-sender="myphone" \
  -webhook-secret="my-super-secret" \
  -sync-remote="gdrive:Research"
```

### Configuration File

You can alternatively pass a single JSON config file (e.g., stored securely in `~/.config/umbilical/config.json`) using the `-config` flag:

```json
{
  "role": "standalone",
  "executorAddress": "smp://...",
  "vaultPath": "/path/to/vault",
  "botProfile": "mybot",
  "simplexPort": 5225,
  "allowedSender": "myphone",
  "webhookSecret": "my-super-secret",
  "syncRemote": "gdrive:Research",
  "syncIntervalMinutes": 15
}
```

```bash
./umbilical -config="/path/to/config.json"
```

## Feedly Webhook Setup & Cloudflare Tunnel

To connect Feedly to your locally-running bot, you need to securely expose the local HTTP server to the internet. We recommend using a **Cloudflare Tunnel**, which allows you to expose the local service without opening inbound firewall ports or needing a static public IP. The daemon establishes an outbound-only connection to Cloudflare's edge, keeping your server hidden and protected from direct external access.

### 1. Setting up `cloudflared`

- **[Installation Guide](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/)**: Download and install the `cloudflared` client.
- **[Tunnels Overview](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/)**: Learn how Cloudflare Tunnels securely route traffic.

You can test the tunnel quickly by running:
```bash
cloudflared tunnel --url http://localhost:8080
```
*(Make sure to copy the generated `https://` URL from the output)*

For permanent setups (so the tunnel survives machine reboots), it is highly recommended to **[Run cloudflared as a service](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/configure-tunnels/local-management/as-a-service/)**:
```bash
sudo cloudflared service install <your-tunnel-token>
sudo systemctl enable --now cloudflared
```

### 2. Configuring the Webhook

Once your tunnel is running:
1. Go to your Feedly Developer Dashboard -> Webhooks.
2. Add a new webhook:
   - **URL**: `<YOUR_CLOUDFLARED_URL>/webhooks/feedly`
   - **Event Type**: `NewEntrySaved`
   - **Authorization Header**: Bearer token matching your `-webhook-secret`.

You can test the webhook locally without Feedly using the provided script:
```bash
./scripts/test_feedly.sh 8080 my-super-secret
```

## Generic Webhook Integration

If you don't have Feedly Pro (which is required for native webhooks) or want to integrate other tools, the bot also exposes a generic webhook endpoint at `/webhooks/generic`.

This endpoint accepts a simpler Bearer token format for authentication instead of Feedly's HMAC signature, making it compatible with almost any automation tool.

### Supported Integrations
Because it relies on standard HTTP POST requests, you can trigger this endpoint using:
- **Make.com** (Free tier available, excellent Feedly/RSS integration)
- **Zapier** or **Pipedream**
- **IFTTT** (Using the Webhooks / "Make a Web Request" action)
- **iOS Shortcuts** (Using "Get contents of URL")
- Any custom script or application

### How to use it:
1. **URL**: `POST <YOUR_CLOUDFLARED_URL>/webhooks/generic`
2. **Headers**:
   - `Content-Type: application/json`
   - `Authorization: Bearer <your-webhook-secret>`
3. **Body (JSON)**:
```json
{
  "title": "Article Title",
  "url": "https://example.com/article",
  "content": "Optional excerpt or summary...",
  "source": "Make.com"
}
```

*Note: The `source` field is optional and defaults to "Webhook".*

## Systemd Service Configuration

It is recommended to run this agent as a systemd service in the background on your local machine.

Create a file named `umbilical.service` in `~/.config/systemd/user/`:

```ini
[Unit]
Description=SimpleX to Obsidian Bot
After=network.target

[Service]
Type=simple
ExecStart=/path/to/umbilical -vault="/path/to/vault" -bot-profile="mybot" -allowed-sender="myphone" -webhook-secret="your-secret" -sync-remote="gdrive:Research"
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
```

Then enable and start the service:

```bash
systemctl --user daemon-reload
systemctl --user enable umbilical.service
systemctl --user start umbilical.service
```

## Dependency Management

This repository uses [Renovate](https://docs.renovatebot.com/) to track out-of-date external dependencies. When new versions of dependencies or modules are released, Renovate will automatically open Pull Requests to keep the project up-to-date and apply any necessary `go mod tidy` actions.
