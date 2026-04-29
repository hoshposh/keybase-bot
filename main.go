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
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"charm.land/log/v2"

	"github.com/hoshposh/umbilical/handler"
	"github.com/hoshposh/umbilical/mcp"
	"github.com/hoshposh/umbilical/pkg/simplex"
	"github.com/hoshposh/umbilical/server"
	syncpkg "github.com/hoshposh/umbilical/sync"
)

type Config struct {
	Role                string `json:"role"`
	ExecutorAddress     string `json:"executorAddress"`
	VaultPath           string `json:"vaultPath"`
	BotProfile          string `json:"botProfile"`
	SimplexPort         int    `json:"simplexPort"`
	AllowedSender       string `json:"allowedSender"`
	HomeDir             string `json:"homeDir"`
	KeepHome            bool   `json:"keepHome"`
	MCPCmd              string `json:"mcpCmd"`
	WebhookPort         int    `json:"webhookPort"`
	WebhookSecret       string `json:"webhookSecret"`
	SyncRemote          string `json:"syncRemote"`
	SyncIntervalMinutes int    `json:"syncIntervalMinutes"`
}

type KeybaseTeamDispatcher struct {
}

func (k *KeybaseTeamDispatcher) Handle(ctx context.Context, msg string) error {
	// Mock implementation
	return nil
}

type BotState struct {
	LastMessageID uint64 `json:"lastMessageId"`
	Channel       string `json:"channel,omitempty"`
}

var version = "dev"

func main() {
	versionFlag := flag.Bool("version", false, "Print version information and exit")
	configPath := flag.String("config", "", "Path to a JSON config file (e.g., in KBFS) to override other flags")
	vaultPath := flag.String("vault", "", "Path to the Obsidian Vault")
	botProfile := flag.String("bot-profile", "", "SimpleX Bot Profile Name")
	simplexPort := flag.Int("simplex-port", 5225, "SimpleX chat websocket daemon port")
	allowedSender := flag.String("allowed-sender", "", "SimpleX Display Name allowed to send commands")
	homeDir := flag.String("home", "", "Path to a dedicated home directory (optional, isolates session)")
	keepHome := flag.Bool("keep-home", false, "Keep the temporary home directory after exit")

	// New headless / MCP flags
	mcpCmdLine := flag.String("mcp-cmd", "npx -y @bitbonsai/mcpvault", "Command to start MCP server")
	webhookPort := flag.Int("webhook-port", 8080, "Port for Feedly webhooks")
	webhookSecret := flag.String("webhook-secret", "", "Secret token for Feedly webhooks authorization")
	syncRemote := flag.String("sync-remote", "", "rclone remote path for sync, e.g., 'gdrive:Research'")
	syncInterval := flag.Duration("sync-interval", 15*time.Minute, "Interval for sync loop")
	setupFlag := flag.Bool("setup", false, "Run the interactive setup wizard")

	// Role splitting flags
	roleFlag := flag.String("role", "standalone", "Role to run: 'standalone', 'ingestor', or 'executor'")
	executorAddressFlag := flag.String("executor-address", "", "SimpleX executor address. Required for ingestor role.")

	flag.Parse()

	if *versionFlag {
		fmt.Printf("umbilical version %s\n", version)
		return
	}

	if *setupFlag {
		runSetup(*configPath)
		return
	}

	if flag.NFlag() == 0 {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			defaultPath := filepath.Join(homeDir, ".config", "umbilical", "config.json")
			if _, err := os.Stat(defaultPath); err == nil {
				*configPath = defaultPath
				log.Infof("No flags provided; automatically loading default config from %s", defaultPath)
			}
		}
	}

	if flag.NFlag() == 0 && *configPath == "" {
		log.Fatal("No configuration arguments provided. Please run explicitly with -setup to launch the configuration wizard, or provide required flags.")
	}

	// Parse config from KBFS or other file if provided
	if *configPath != "" {
		data, err := os.ReadFile(*configPath) //nolint:gosec // path is a user-supplied CLI flag
		if err != nil {
			log.Fatalf("Failed to read config file at %s: %v", *configPath, err)
		}
		var c Config
		if err := json.Unmarshal(data, &c); err != nil {
			log.Fatalf("Failed to parse config file: %v", err)
		}

		if c.Role != "" && *roleFlag == "standalone" {
			*roleFlag = c.Role
		}
		if c.ExecutorAddress != "" {
			*executorAddressFlag = c.ExecutorAddress
		}
		if c.VaultPath != "" {
			*vaultPath = c.VaultPath
		}
		if c.BotProfile != "" {
			*botProfile = c.BotProfile
		}
		if c.SimplexPort != 0 {
			*simplexPort = c.SimplexPort
		}
		if c.AllowedSender != "" {
			*allowedSender = c.AllowedSender
		}
		if c.HomeDir != "" {
			*homeDir = c.HomeDir
		}
		if c.KeepHome {
			*keepHome = c.KeepHome
		}
		if c.MCPCmd != "" {
			*mcpCmdLine = c.MCPCmd
		}
		if c.WebhookPort != 0 {
			*webhookPort = c.WebhookPort
		}
		if c.WebhookSecret != "" {
			*webhookSecret = c.WebhookSecret
		}
		if c.SyncRemote != "" {
			*syncRemote = c.SyncRemote
		}
		if c.SyncIntervalMinutes != 0 {
			*syncInterval = time.Duration(c.SyncIntervalMinutes) * time.Minute
		}
	}

	if *botProfile == "" {
		log.Fatal("-bot-profile is required")
	}

	isStandalone := *roleFlag == "standalone"
	isIngestor := *roleFlag == "ingestor"
	isExecutor := *roleFlag == "executor"

	if !isStandalone && !isIngestor && !isExecutor {
		log.Fatal("-role must be 'standalone', 'ingestor', or 'executor'")
	}

	if isStandalone || isExecutor {
		if *vaultPath == "" {
			log.Fatal("-vault is required for standalone and executor roles")
		}
	}
	if isStandalone || isIngestor {
		if *allowedSender == "" {
			log.Fatal("-allowed-sender is required for standalone and ingestor roles")
		}
	}
	if isIngestor {
		if *executorAddressFlag == "" {
			log.Fatal("-executor-address is required for ingestor role")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Build profile directory path consistent with setup wizard.
	homeDirVal, _ := os.UserHomeDir()
	profileDir := filepath.Join(homeDirVal, ".config", "umbilical", "profiles", *botProfile)

	// --- Dispatcher / MCP / Webhooks ---
	var dispatcher server.MessageDispatcher
	var msgHandler *handler.MessageHandler
	var httpServer *http.Server

	if isStandalone || isExecutor {
		cmdParts := strings.Fields(*mcpCmdLine)
		mcpClient, err := mcp.NewClient(ctx, cmdParts, *vaultPath)
		if err != nil {
			log.Fatalf("Failed to initialize MCP Client: %v", err)
		}
		defer func() {
			if err := mcpClient.Close(); err != nil {
				log.Errorf("MCP client close: %v", err)
			}
		}()
		log.Printf("MCP client initialized successfully.")

		msgHandler = handler.NewMessageHandler(*vaultPath, mcpClient)
		dispatcher = msgHandler

		if *syncRemote != "" {
			driveSync := syncpkg.NewDriveSync(*vaultPath, *syncRemote, *syncInterval)
			go driveSync.Start(ctx)
		}
	}

	if isStandalone || isIngestor {
		if dispatcher == nil {
			dispatcher = msgHandler
		}
		httpServer = server.StartWebhookServer(*webhookPort, *webhookSecret, dispatcher)
	}

	// --- SimpleX daemon + listener ---
	// The SimpleX WebSocket is the primary channel for Android client → vault communication.
	// It is required for standalone and executor roles.
	simplexClient, err := simplex.NewClient(ctx, profileDir, *simplexPort)
	if err != nil {
		log.Fatalf("Failed to start SimpleX client: %v", err)
	}
	defer func() {
		if err := simplexClient.Close(); err != nil {
			log.Errorf("SimpleX client close: %v", err)
		}
	}()

	if isStandalone || isExecutor {
		go func() {
			err := simplexClient.Listen(func(msg simplex.IncomingMessage) {
				if msg.SenderName != *allowedSender {
					log.Debugf("Ignoring message from unauthorized sender: %s", msg.SenderName)
					return
				}
				log.Infof("SimpleX message from %s: %s", msg.SenderName, msg.Text)
				if err := dispatcher.Handle(ctx, msg.Text); err != nil {
					log.Errorf("Failed to handle message: %v", err)
				}
			})
			if err != nil {
				log.Errorf("SimpleX listener exited: %v", err)
				cancel()
			}
		}()
	}

	if isStandalone {
		printDashboard(*botProfile, *allowedSender, *vaultPath, *webhookPort, *webhookSecret, *syncRemote)
	}
	log.Printf("Listening for SimpleX messages on role: %s...", *roleFlag)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Print("Shutting down...")
	cancel() // context cancellation will shut down mcp, http, and sync loop

	if httpServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Errorf("HTTP server shutdown: %v", err)
		}
	}

	os.Exit(0)
}
