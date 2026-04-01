# Future Enhancements Plan

This document serves as a record of feature ideas and architectural changes planned for future iterations of the `keybase-obsidian-bot`.

---

## 1. Obsidian CLI Integration

**Concept:** 
Utilize the Obsidian CLI to interact with vaults rather than writing files directly via standard OS file operations. 

**Advantages:**
- The program no longer needs to make direct changes to Obsidian markdown files, delegating file management and formatting to Obsidian itself.
- Built-in commands like `daily:append` handle standard Obsidian features natively without the bot needing to replicate date-formatting logic.

**Disadvantages:**
- The main Obsidian desktop application must be actively running on the host machine where the bot is deployed.

**Implementation Details:**
- **Route `!note`:** Execute `obsidian vault="<VaultName>" append file="<InboxFile>" content="<text>"`
- **Route `!todo`:** Execute `obsidian vault="<VaultName>" append file="<TasksFile>" content="<text>"`
- **Route Daily Notes:** Execute `obsidian vault="<VaultName>" daily:append content="<text>"`
- **Error Handling:** If the CLI responds with the error `"Unable to connect to main process"`, the bot should intercept this and return a friendly message indicating that the main Obsidian program is not currently running.

**Argument Changes Needed:**
- The `-vault` CLI flag, which currently expects an absolute file path (e.g., `/path/to/vault`), will need to be updated (or replaced with a `-vault-name` flag) to accept the Vault's Name or ID. The Obsidian CLI targets vaults using `vault="My Vault"` rather than an absolute filesystem path. The bot should also validate this argument on startup using the CLI command meant to get information about a vault.
- Add an `-inbox-file` argument (defaulting to `Inbox.md`) allowing users to override the file used by the `!note` command.
- Add a `-tasks-file` argument (defaulting to `Tasks.md`) allowing users to override the file used by the `!todo` command.
