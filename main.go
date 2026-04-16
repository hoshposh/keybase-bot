/*
 * Copyright (c) 2026 Lyndon Washington
 * Licensed under the MIT License. See LICENSE in the project root for license information.
 */

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/hoshposh/keybase-obsidian-bot/handler"
	"github.com/hoshposh/keybase-obsidian-bot/mcp"
	"github.com/hoshposh/keybase-obsidian-bot/server"
	"github.com/hoshposh/keybase-obsidian-bot/sync"
	"github.com/keybase/go-keybase-chat-bot/kbchat"
	"github.com/keybase/go-keybase-chat-bot/kbchat/types/chat1"
)

type Config struct {
	VaultPath           string `json:"vaultPath"`
	BotUsername         string `json:"botUsername"`
	SecretPath          string `json:"secretPath"`
	AllowedSender       string `json:"allowedSender"`
	HomeDir             string `json:"homeDir"`
	KeepHome            bool   `json:"keepHome"`
	MCPCmd              string `json:"mcpCmd"`
	WebhookPort         int    `json:"webhookPort"`
	WebhookSecret       string `json:"webhookSecret"`
	SyncRemote          string `json:"syncRemote"`
	SyncIntervalMinutes int    `json:"syncIntervalMinutes"`
}

type BotState struct {
	LastMessageID chat1.MessageID `json:"lastMessageId"`
}

func loadState(path string) BotState {
	var s BotState
	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &s)
	}
	return s
}

func saveState(path string, s BotState) {
	data, err := json.Marshal(s)
	if err == nil {
		os.WriteFile(path, data, 0644)
	}
}

func main() {
	configPath := flag.String("config", "", "Path to a JSON config file (e.g., in KBFS) to override other flags")
	vaultPath := flag.String("vault", "", "Path to the Obsidian Vault")
	botUsername := flag.String("bot-username", "", "Bot Username")
	secretPath := flag.String("secret-path", "", "Path to the paper key")
	allowedSender := flag.String("allowed-sender", "", "Username allowed to send commands")
	homeDir := flag.String("home", "", "Path to a dedicated Keybase home directory (optional, isolates session)")
	keepHome := flag.Bool("keep-home", false, "Keep the temporary home directory after exit")

	// New headless / MCP flags
	mcpCmdLine := flag.String("mcp-cmd", "npx -y @bitbonsai/mcpvault", "Command to start MCP server")
	webhookPort := flag.Int("webhook-port", 8080, "Port for Feedly webhooks")
	webhookSecret := flag.String("webhook-secret", "", "Secret token for Feedly webhooks authorization")
	syncRemote := flag.String("sync-remote", "", "rclone remote path for sync, e.g., 'gdrive:Research'")
	syncInterval := flag.Duration("sync-interval", 15*time.Minute, "Interval for sync loop")

	flag.Parse()

	// Parse config from KBFS or other file if provided
	if *configPath != "" {
		data, err := os.ReadFile(*configPath)
		if err != nil {
			log.Fatalf("Failed to read config file at %s: %v", *configPath, err)
		}
		var c Config
		if err := json.Unmarshal(data, &c); err != nil {
			log.Fatalf("Failed to parse config file: %v", err)
		}

		if c.VaultPath != "" { *vaultPath = c.VaultPath }
		if c.BotUsername != "" { *botUsername = c.BotUsername }
		if c.SecretPath != "" { *secretPath = c.SecretPath }
		if c.AllowedSender != "" { *allowedSender = c.AllowedSender }
		if c.HomeDir != "" { *homeDir = c.HomeDir }
		if c.KeepHome { *keepHome = c.KeepHome }
		if c.MCPCmd != "" { *mcpCmdLine = c.MCPCmd }
		if c.WebhookPort != 0 { *webhookPort = c.WebhookPort }
		if c.WebhookSecret != "" { *webhookSecret = c.WebhookSecret }
		if c.SyncRemote != "" { *syncRemote = c.SyncRemote }
		if c.SyncIntervalMinutes != 0 { *syncInterval = time.Duration(c.SyncIntervalMinutes) * time.Minute }
	}

	if *vaultPath == "" || *botUsername == "" || *secretPath == "" || *allowedSender == "" {
		log.Fatal("All core configuration properties (-vault, -bot-username, -secret-path, -allowed-sender) are required")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Setup MCP Client
	cmdParts := strings.Fields(*mcpCmdLine)
	mcpClient, err := mcp.NewClient(ctx, cmdParts, *vaultPath)
	if err != nil {
		log.Fatalf("Failed to initialize MCP Client: %v", err)
	}
	defer mcpClient.Close()
	log.Printf("MCP client initialized successfully.")

	// 2. Setup Message Handler (now using MCP)
	msgHandler := handler.NewMessageHandler(*vaultPath, mcpClient)

	// 3. Setup Webhook server (Feedly)
	httpServer := server.StartWebhookServer(*webhookPort, *webhookSecret, msgHandler)

	// 4. Setup Google Drive Sync Loop
	if *syncRemote != "" {
		driveSync := sync.NewDriveSync(*vaultPath, *syncRemote, *syncInterval)
		go driveSync.Start(ctx)
	}

	// 5. Setup Keybase Listeners
	paperKeyBytes, err := os.ReadFile(*secretPath)
	if err != nil {
		log.Fatalf("Failed to read secret path: %v", err)
	}
	paperKey := strings.TrimSpace(string(paperKeyBytes))

	createdTempDir := ""
	actualHome := *homeDir
	if actualHome == "" {
		tmp, err := os.MkdirTemp("", "keybase-bot-home-*")
		if err != nil {
			log.Fatalf("Failed to create temporary home directory: %v", err)
		}
		actualHome = tmp
		createdTempDir = tmp
		log.Printf("Created temporary home directory: %s", actualHome)
	}

	cleanup := func() {
		if !*keepHome && createdTempDir != "" {
			log.Printf("Cleaning up temporary home directory: %s", createdTempDir)
			os.RemoveAll(createdTempDir)
		}
	}
	defer cleanup()

	opts := kbchat.RunOptions{
		KeybaseLocation: "keybase",
		HomeDir:         actualHome,
		Oneshot: &kbchat.OneshotOptions{
			Username: *botUsername,
			PaperKey: paperKey,
		},
	}

	kbc, err := kbchat.Start(opts)
	if err != nil {
		log.Fatalf("Error starting Keybase chat client: %v", err)
	}

	stateFile := filepath.Join(*vaultPath, ".keybase_bot_state.json")
	botState := loadState(stateFile)

	channel := chat1.ChatChannel{
		Name: fmt.Sprintf("%s,%s", *botUsername, *allowedSender),
	}

	missedMessages, err := kbc.GetTextMessages(channel, false)
	if err != nil {
		log.Printf("Warning: failed to get message history: %v", err)
	} else {
		// Messages usually from newest to oldest or vice versa. Let's process ones > LastMessageID chronologically.
		// So we collect and process them in order of MsgID ascending.
		var toProcess []chat1.MsgSummary
		for _, m := range missedMessages {
			if m.Id > botState.LastMessageID && m.Sender.Username == *allowedSender && m.Content.TypeName == "text" {
				toProcess = append(toProcess, m)
			}
		}
		// Sort just in case
		for i := 0; i < len(toProcess); i++ {
			for j := i + 1; j < len(toProcess); j++ {
				if toProcess[i].Id > toProcess[j].Id {
					toProcess[i], toProcess[j] = toProcess[j], toProcess[i]
				}
			}
		}

		for _, m := range toProcess {
			body := m.Content.Text.Body
			log.Printf("Processing missed Keybase message (ID: %d): '%s'", m.Id, body)
			if err := msgHandler.Handle(ctx, body); err != nil {
				log.Printf("Error handling missed Keybase message '%s': %v", body, err)
			} else {
				log.Printf("Successfully handled missed Keybase message '%s'", body)
				botState.LastMessageID = m.Id
				saveState(stateFile, botState)
			}
		}
	}

	sub, err := kbc.ListenForNewTextMessages()
	if err != nil {
		log.Fatalf("Error listening for messages: %v", err)
	}

	log.Printf("Listening for Keybase messages... (allowed sender: %s)", *allowedSender)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		cancel() // context cancellation will shut down mcp, http, and sync loop
		cleanup()
		
		// shutdown http server gracefully
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		httpServer.Shutdown(shutdownCtx)
		
		os.Exit(0)
	}()

	for {
		msg, err := sub.Read()
		if err != nil {
			log.Printf("Error reading message: %v", err)
			continue
		}

		if msg.Message.Sender.Username != *allowedSender {
			continue // Ignore messages from other senders
		}
		
		if msg.Message.Content.TypeName != "text" {
			continue
		}

		body := msg.Message.Content.Text.Body
		if err := msgHandler.Handle(ctx, body); err != nil {
			log.Printf("Error handling Keybase message '%s': %v", body, err)
		} else {
			log.Printf("Successfully handled Keybase message '%s'", body)
			// Update state
			if msg.Message.Id > botState.LastMessageID {
				botState.LastMessageID = msg.Message.Id
				saveState(stateFile, botState)
			}
		}
	}
}
