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
	"sort"
	"strings"
	"syscall"
	"time"

	"charm.land/log/v2"

	"github.com/hoshposh/keybase-obsidian-bot/handler"
	"github.com/hoshposh/keybase-obsidian-bot/mcp"
	"github.com/hoshposh/keybase-obsidian-bot/server"
	"github.com/hoshposh/keybase-obsidian-bot/sync"
	"github.com/keybase/go-keybase-chat-bot/kbchat"
	"github.com/keybase/go-keybase-chat-bot/kbchat/types/chat1"
)

type Config struct {
	Role                string `json:"role"`
	JobChannel          string `json:"jobChannel"`
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

type KeybaseTeamDispatcher struct {
	kbc     *kbchat.API
	channel chat1.ChatChannel
}

func (k *KeybaseTeamDispatcher) Handle(ctx context.Context, msg string) error {
	_, err := k.kbc.SendMessage(k.channel, "%s", msg)
	return err
}

type BotState struct {
	LastMessageID chat1.MessageID `json:"lastMessageId"`
	Channel       string          `json:"channel,omitempty"`
}

var version = "dev"

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
	versionFlag := flag.Bool("version", false, "Print version information and exit")
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
	setupFlag := flag.Bool("setup", false, "Run the interactive setup wizard")

	// Role splitting flags
	roleFlag := flag.String("role", "standalone", "Role to run: 'standalone', 'ingestor', or 'executor'")
	jobChannelFlag := flag.String("job-channel", "", "Keybase team channel for jobs (e.g. 'myteam.jobs'). Required for executor and ingestor roles.")

	flag.Parse()

	if *versionFlag {
		fmt.Printf("keybase-bot version %s\n", version)
		return
	}

	if *setupFlag {
		runSetup(*configPath)
		return
	}

	if flag.NFlag() == 0 {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			defaultPath := filepath.Join(homeDir, ".config", "keybase-bot", "config.json")
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
		data, err := os.ReadFile(*configPath)
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
		if c.JobChannel != "" {
			*jobChannelFlag = c.JobChannel
		}
		if c.VaultPath != "" {
			*vaultPath = c.VaultPath
		}
		if c.BotUsername != "" {
			*botUsername = c.BotUsername
		}
		if c.SecretPath != "" {
			*secretPath = c.SecretPath
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

	if *botUsername == "" || *secretPath == "" {
		log.Fatal("-bot-username and -secret-path are required")
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
	if isIngestor || isExecutor {
		if *jobChannelFlag == "" {
			log.Fatal("-job-channel is required for ingestor and executor roles")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Extract keybase setup logic to be common
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

	var dispatcher server.MessageDispatcher
	var msgHandler *handler.MessageHandler
	var httpServer *http.Server

	if isStandalone || isExecutor {
		cmdParts := strings.Fields(*mcpCmdLine)
		mcpClient, err := mcp.NewClient(ctx, cmdParts, *vaultPath)
		if err != nil {
			log.Fatalf("Failed to initialize MCP Client: %v", err)
		}
		defer mcpClient.Close()
		log.Printf("MCP client initialized successfully.")

		msgHandler = handler.NewMessageHandler(*vaultPath, mcpClient)
		dispatcher = msgHandler

		if *syncRemote != "" {
			driveSync := sync.NewDriveSync(*vaultPath, *syncRemote, *syncInterval)
			go driveSync.Start(ctx)
		}
	}

	var parseJobChannel = func(full string) chat1.ChatChannel {
		parts := strings.SplitN(full, ".", 2)
		if len(parts) == 2 {
			return chat1.ChatChannel{
				Name:        parts[0],
				TopicName:   parts[1],
				MembersType: "team",
			}
		}
		return chat1.ChatChannel{Name: full}
	}

	if isIngestor {
		dispatcher = &KeybaseTeamDispatcher{
			kbc:     kbc,
			channel: parseJobChannel(*jobChannelFlag),
		}
	}

	if isStandalone || isIngestor {
		httpServer = server.StartWebhookServer(*webhookPort, *webhookSecret, dispatcher)
	}

	var stateFile string
	var botState BotState
	if isStandalone || isExecutor {
		stateFile = filepath.Join(*vaultPath, ".keybase_bot_state.json")
	} else if isIngestor {
		if *configPath != "" {
			stateFile = filepath.Join(filepath.Dir(*configPath), ".keybase_ingestor_state.json")
		} else {
			homeDir, _ := os.UserHomeDir()
			configDir := filepath.Join(homeDir, ".config", "keybase-bot")
			os.MkdirAll(configDir, 0755)
			stateFile = filepath.Join(configDir, ".keybase_ingestor_state.json")
		}
	}
	botState = loadState(stateFile)

	var listenChannel chat1.ChatChannel
	if isStandalone || isIngestor {
		listenChannel = chat1.ChatChannel{
			Name: fmt.Sprintf("%s,%s", *botUsername, *allowedSender),
		}
	} else {
		// executor listens on job channel
		listenChannel = parseJobChannel(*jobChannelFlag)
	}

	if listenChannel.MembersType == "team" {
		log.Printf("Ensuring bot is joined to team channel %s#%s...", listenChannel.Name, listenChannel.TopicName)
		if _, err := kbc.JoinChannel(listenChannel.Name, listenChannel.TopicName); err != nil {
			log.Printf("Note: failed to explicitly join channel (it might be a small team or you lack permissions): %v", err)
		}
	}

	listenChannelKey := listenChannel.Name
	if listenChannel.TopicName != "" {
		listenChannelKey += "#" + listenChannel.TopicName
	}

	if stateFile != "" {
		if botState.Channel != "" && botState.Channel != listenChannelKey {
			log.Printf("Detected job channel switched from %s to %s. Resetting LastMessageID sequence.", botState.Channel, listenChannelKey)
			botState.LastMessageID = 0
		}
		botState.Channel = listenChannelKey
		saveState(stateFile, botState)
	}

	channelMatch := func(a, b chat1.ChatChannel) bool {
		if a.MembersType == "team" || b.MembersType == "team" {
			return a.Name == b.Name && a.TopicName == b.TopicName
		}
		// DMs are returned as comma separated strings which can differ in order
		an := strings.Split(a.Name, ",")
		bn := strings.Split(b.Name, ",")
		sort.Strings(an)
		sort.Strings(bn)
		return strings.Join(an, ",") == strings.Join(bn, ",")
	}

	missedMessages, err := kbc.GetTextMessages(listenChannel, false)
	if err != nil {
		log.Printf("Warning: failed to get message history: %v", err)
	} else {
		// Messages usually from newest to oldest or vice versa. Let's process ones > LastMessageID chronologically.
		// So we collect and process them in order of MsgID ascending.
		var toProcess []chat1.MsgSummary
		for _, m := range missedMessages {
			validSender := isExecutor || (m.Sender.Username == *allowedSender)
			if stateFile != "" && m.Id > botState.LastMessageID && validSender && m.Content.TypeName == "text" {
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
			if err := dispatcher.Handle(ctx, body); err != nil {
				log.Printf("Error handling missed Keybase message '%s': %v", body, err)
			} else {
				log.Printf("Successfully handled missed Keybase message '%s'", body)
				if stateFile != "" {
					botState.LastMessageID = m.Id
					saveState(stateFile, botState)
				}
			}
		}
	}

	sub, err := kbc.ListenForNewTextMessages()
	if err != nil {
		log.Fatalf("Error listening for messages: %v", err)
	}

	if isStandalone {
		printDashboard(*botUsername, *allowedSender, *vaultPath, *webhookPort, *webhookSecret, *syncRemote)
	}
	log.Printf("Listening for Keybase messages on role: %s...", *roleFlag)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Print("Shutting down...")
		cancel() // context cancellation will shut down mcp, http, and sync loop
		cleanup()

		if httpServer != nil {
			// shutdown http server gracefully
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			httpServer.Shutdown(shutdownCtx)
		}

		os.Exit(0)
	}()

	for {
		msg, err := sub.Read()
		if err != nil {
			log.Printf("Error reading message: %v", err)
			continue
		}

		if !isExecutor && msg.Message.Sender.Username != *allowedSender {
			continue // Ignore messages from other senders
		}

		if msg.Message.Channel.Name == "" {
			continue
		}

		if !channelMatch(msg.Message.Channel, listenChannel) {
			continue // Ignore messages in other channels not configured for this listener
		}

		if msg.Message.Content.TypeName != "text" {
			continue
		}

		body := msg.Message.Content.Text.Body
		if err := dispatcher.Handle(ctx, body); err != nil {
			log.Printf("Error handling Keybase message '%s': %v", body, err)
		} else {
			log.Printf("Successfully handled Keybase message '%s'", body)
			// Update state
			if stateFile != "" && msg.Message.Id > botState.LastMessageID {
				botState.LastMessageID = msg.Message.Id
				saveState(stateFile, botState)
			}
		}
	}
}
