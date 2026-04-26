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

## Deployment Architecture (Roles)

The bot can be executed in three different deployment modes via the `-role` flag, allowing you to establish a secure, remote-friendly architecture:

1. **Standalone** (`-role=standalone`): The default mode. Runs all features (Webhooks, Keybase Listeners, MCP, and Sync) in a single local process.
2. **Cloud Ingestor** (`-role=ingestor`): A secure gateway designed to run on a cloud server. It exposes the webhook HTTP server and forwards inbound payload data straight into a specified Keybase Team Channel. It does not perform any local MCP or Vault commands.
3. **Local Executor** (`-role=executor`): Runs locally behind your firewall. It listens to the Keybase Team Channel for "jobs" sent by the Ingestor and safely executes those payloads into your local Obsidian Vault using MCP.

By splitting the architecture, your Vault remains entirely air-gapped from the public Internet while you can still host the webhook ingestor anywhere.

## Keybase Setup Guide

Whether you run in standalone or split mode, you need Keybase bot accounts and paper keys. If using the split `ingestor` and `executor` roles, you will also need a dedicated Keybase team channel to bridge them.

### 1. Create the Keybase Bot Accounts

> [!NOTE]
> If you are setting up the split architecture with two separate bot accounts (e.g., `my_bot_ingest` and `my_bot_exec`), **you must run these 4 steps twice**—once for each bot to generate their unique paper keys.

To go from "No Bot" to having a **Paper Key** ready for your Go code, follow this specific command-line sequence. We recommend using a temporary home directory (`/tmp/bot_home`) during this process to ensure the bot's setup doesn't interfere with your personal Keybase session.

#### Step 1: Generate the Authorization Token
First, as your **main user**, generate the token that gives you permission to create a bot.
```bash
keybase bot token create > /tmp/bot_token.txt
```
*(This saves a base64 string to a temporary file).*

#### Step 2: Create the Bot Account
Now, run the signup command. We use `--home` to keep it isolated and `--standalone` so it doesn't try to start a permanent background service yet.
```bash
keybase --home=/tmp/bot_home --standalone bot signup \
    -u my_bot_ingest \
    -t $(cat /tmp/bot_token.txt) > /tmp/bot_paper_key.txt
```
Keybase creates the account `@my_bot_ingest` and saves your new **Paper Key** to `/tmp/bot_paper_key.txt`.

#### Step 3: Capture the Paper Key
Open that file and copy the multi-word phrase inside.
```bash
cat /tmp/bot_paper_key.txt
```
> [!WARNING]
> This is the only time this key is shown. If you lose it, you lose access to the bot account. **Copy it now** and store it in your KBFS private folder (or pass it via the `-secret-path` flag to authenticate your instances).

#### Step 4: Clean Up
Delete the temporary files from your `/tmp` directory securely.
```bash
rm /tmp/bot_token.txt /tmp/bot_paper_key.txt
rm -rf /tmp/bot_home
```

### 2. Create the Job Channel (Split Architecture)

To implement the **Satellite Pattern** correctly, use a Keybase Team. Teams provide the persistence and administrative control needed to keep your Cloud Ingestor and Local Executor in sync securely.

#### Step 1: Create a Private Team
First, create a dedicated team for your automation (e.g. `my_automation`).
```bash
keybase team create my_automation
```

#### Step 2: Create the Communication Channel
By default, every team has a `#general` channel. It is better to create a specific channel for the bot traffic to keep it clean.
```bash
keybase chat send --channel '#vault-ingress' my_automation "Initializing bot channel..."
```

#### Step 3: Add the Two Bot Accounts
Add both your Cloud Bot and your Local Bot to this team with specific security roles.

- **For the Cloud Ingestor (Bot):**
  This role allows the bot to write payloads efficiently and resolve channel names cryptographically without granting it full human administrative `writer` rights over the team files.
  ```bash
  keybase team add-member my_automation --user my_bot_ingest --role bot
  ```

- **For the Local Executor (Writer):**
  This bot remains local and requires full "Writer" permissions to continuously read the history and dequeue the processed messages.
  ```bash
  keybase team add-member my_automation --user my_bot_exec --role writer
  ```

When you launch the binaries, you will supply this channel string (e.g., `my_automation.vault-ingress`) as the `-job-channel` argument.

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
- `-job-channel`: (Required for `ingestor`/`executor`) The Keybase team and channel where jobs are passed (e.g., `myteam.jobs`).
- `-vault`: (Required for `standalone`/`executor`) The absolute path to your Obsidian vault.
- `-bot-username`: The username of the Keybase bot/account.
- `-secret-path`: The file path to a text file containing exactly the Keybase Paper Key.
- `-allowed-sender`: (Required for `standalone`/`ingestor`) The Keybase username of the person allowed to send commands to the bot.
- `-mcp-cmd`: (Optional) The command to run the MCP server. Default is `npx -y @bitbonsai/mcpvault`.
- `-webhook-port`: (Optional) Port for the Feedly webhook HTTP server. Default is `8080`.
- `-webhook-secret`: (Optional) A shared secret token to expect in the `Authorization: Bearer <secret>` header from Feedly.
- `-sync-remote`: (Optional) If provided, initiates a background rclone sync loop. Example: `gdrive:ObsidianResearch`.
- `-sync-interval`: (Optional) Duration between syncs. Default is `15m`.

You can easily step through a configuration wizard that will generate your `config.json` by running:

```bash
./keybase-obsidian-bot -setup
```

Or you can run the bot bypassing the wizard entirely via CLI arguments:

```bash
./keybase-obsidian-bot \
  -role="standalone" \
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
  "role": "standalone",
  "jobChannel": "",
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

## Dependency Management

This repository uses [Renovate](https://docs.renovatebot.com/) to track out-of-date external dependencies. When new versions of dependencies or modules are released, Renovate will automatically open Pull Requests to keep the project up-to-date and apply any necessary `go mod tidy` actions.
