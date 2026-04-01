# Keybase to Obsidian Bot

A lightweight Go headless service to bridge messages from Keybase and Feedly to a local Obsidian vault using the Model Context Protocol (MCP).

## Features

- **MCP Integration**: Uses the Model Context Protocol (`mcpvault`) to safely append notes to your Obsidian vault without manual format hacking.
- **Dual-Input Gateway**:
    - **Keybase Chat**: Securely receive messages from a trusted Keybase sender.
    - **Feedly Webhooks**: Save articles to your Obsidian vault from Feedly.
- **Prefix Routing (via Keybase)**:
    - `!note [text]` appends to `[VaultPath]/Inbox.md`
    - `!todo [text]` appends to `[VaultPath]/Tasks.md`
    - `!link [url]` appends to `[VaultPath]/Links.md`
    - Any message starting with `http://` or `https://` (and containing no spaces) also appends to `[VaultPath]/Links.md`.
    - No prefix appends to `[VaultPath]/Daily/YYYY-MM-DD.md`
- **Google Drive Sync**: Automatically syncs your `/Research` directory to Google Drive (for NotebookLM grounding) securely via `rclone`.

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
- `-vault`: The absolute path to your Obsidian vault.
- `-bot-username`: The username of the Keybase bot/account.
- `-secret-path`: The file path to a text file containing exactly the Keybase Paper Key.
- `-allowed-sender`: The Keybase username of the person allowed to send commands to the bot.
- `-mcp-cmd`: (Optional) The command to run the MCP server. Default is `npx -y @bitbonsai/mcpvault`.
- `-webhook-port`: (Optional) Port for the Feedly webhook HTTP server. Default is `8080`.
- `-webhook-secret`: (Optional) A shared secret token to expect in the `Authorization: Bearer <secret>` header from Feedly.
- `-sync-remote`: (Optional) If provided, initiates a background rclone sync loop. Example: `gdrive:ObsidianResearch`.
- `-sync-interval`: (Optional) Duration between syncs. Default is `15m`.

You can run the bot by passing CLI arguments:

```bash
./keybase-obsidian-bot \
  -vault="/path/to/vault" \
  -bot-username="mybot" \
  -secret-path="/path/to/paperkey.txt" \
  -allowed-sender="myusername" \
  -webhook-secret="my-super-secret" \
  -sync-remote="gdrive:Research"
```

### KBFS Config File

You can alternatively pass a single JSON config file (e.g., stored securely in your private KBFS folder `keybase/private/username/config.json`) using the `-config` flag:

```json
{
  "vaultPath": "/path/to/vault",
  "botUsername": "mybot",
  "secretPath": "/path/to/paperkey.txt",
  "allowedSender": "myusername",
  "webhookSecret": "my-super-secret",
  "syncRemote": "gdrive:Research",
  "syncIntervalMinutes": 15
}
```

```bash
./keybase-obsidian-bot -config="/path/to/config.json"
```

## Feedly Webhook Setup & Cloudflare Tunnel

To connect Feedly to your locally-running bot, you need to expose the local HTTP server to the internet using a tunnel like Cloudflare Tunnel (`cloudflared`).

1. Install `cloudflared`.
2. Run `cloudflared` to expose the webhook port (default `8080`):
   ```bash
   cloudflared tunnel --url http://localhost:8080
   ```
3. Copy the generated `https://` URL.
4. Go to your Feedly Developer Dashboard -> Webhooks.
5. Add a new webhook:
   - **URL**: `<YOUR_CLOUDFLARED_URL>/webhooks/feedly`
   - **Event Type**: `NewEntrySaved`
   - **Authorization Header**: Bearer token matching your `-webhook-secret`.

You can test the webhook locally without Feedly using the provided script:
```bash
./scripts/test_feedly.sh 8080 my-super-secret
```

## Systemd Service Configuration

It is recommended to run this agent as a systemd service in the background on your local machine.

Create a file named `keybase-obsidian-bot.service` in `~/.config/systemd/user/`:

```ini
[Unit]
Description=Keybase to Obsidian Bot
After=network.target

[Service]
Type=simple
ExecStart=/path/to/keybase-obsidian-bot -vault="/path/to/vault" -bot-username="mybot" -secret-path="/path/to/paperkey.txt" -allowed-sender="myusername" -webhook-secret="your-secret" -sync-remote="gdrive:Research"
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
```

Then enable and start the service:

```bash
systemctl --user daemon-reload
systemctl --user enable keybase-obsidian-bot.service
systemctl --user start keybase-obsidian-bot.service
```
