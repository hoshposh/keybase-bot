package handler

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// MCPClient defines the interface for calling MCP tools
type MCPClient interface {
	CallTool(ctx context.Context, name string, args map[string]interface{}) ([]byte, error)
}

// MessageHandler handles routing incoming messages to the appropriate Obsidian file via MCP.
type MessageHandler struct {
	VaultPath string
	Now       func() time.Time
	MCPClient MCPClient
}

// NewMessageHandler creates a new MessageHandler.
func NewMessageHandler(vaultPath string, mcpClient MCPClient) *MessageHandler {
	return &MessageHandler{
		VaultPath: vaultPath,
		Now:       time.Now,
		MCPClient: mcpClient,
	}
}

// Handle processes a message and routes it to the correct note using MCP.
func (h *MessageHandler) Handle(ctx context.Context, msg string) error {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return nil
	}

	var destFile string
	var content string

	if strings.HasPrefix(msg, "!note ") {
		content = strings.TrimPrefix(msg, "!note ")
		destFile = "Inbox.md"
	} else if strings.HasPrefix(msg, "!todo ") {
		content = "- [ ] " + strings.TrimPrefix(msg, "!todo ")
		destFile = "Tasks.md"
	} else if strings.HasPrefix(msg, "!link ") {
		content = strings.TrimPrefix(msg, "!link ")
		destFile = "Links.md"
	} else if (strings.HasPrefix(msg, "http://") || strings.HasPrefix(msg, "https://")) && !strings.Contains(msg, " ") {
		content = msg
		destFile = "Links.md"
	} else {
		content = msg
		dateStr := h.Now().Format("2006-01-02")
		// Assuming the MCP server is rooted at the vault path
		destFile = filepath.Join("Daily", dateStr+".md")
	}

	// Call mcpvault tool to append content
	// We use "append_content" as the generic MCP tool name for modifying files
	args := map[string]interface{}{
		"path":    destFile,
		"content": content + "\n",
	}

	_, err := h.MCPClient.CallTool(ctx, "append_content", args)
	if err != nil {
		return fmt.Errorf("failed to call MCP append_content for %s: %w", destFile, err)
	}

	return nil
}
