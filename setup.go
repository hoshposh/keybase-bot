/*
 * Copyright (c) 2026 Lyndon Washington
 * Licensed under the MIT License. See LICENSE in the project root for license information.
 */

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"charm.land/log/v2"
	"charm.land/huh/v2"
)

func runSetup() {
	var c Config
	var enableWebhooks bool
	var enableSync bool

	homeDir, _ := os.UserHomeDir()
	defaultConfigPath := filepath.Join(homeDir, ".config", "keybase-bot", "config.json")
	var configPath = defaultConfigPath

	var webhookPortStr = "8080"
	var syncIntervalStr = "15"

	log.Print("Welcome to the Keybase Obsidian Bot Setup Wizard!")

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Obsidian Vault Path").
				Description("Absolute path to your Obsidian vault").
				Value(&c.VaultPath).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("vault path cannot be empty")
					}
					return nil
				}),
			huh.NewInput().
				Title("Keybase Bot Username").
				Description("Username for the Keybase bot").
				Value(&c.BotUsername).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("bot username cannot be empty")
					}
					return nil
				}),
			huh.NewInput().
				Title("Keybase Paper Key Path").
				Description("Path to a file containing your bot's paper key").
				Value(&c.SecretPath).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("secret path cannot be empty")
					}
					return nil
				}),
			huh.NewInput().
				Title("Allowed Sender Username").
				Description("Keybase username allowed to message the bot").
				Value(&c.AllowedSender).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("allowed sender cannot be empty")
					}
					return nil
				}),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable Webhooks?").
				Description("This allows tools like Feedly to send links to your vault").
				Value(&enableWebhooks),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Webhook Port").
				Description("Port to listen for webhooks").
				Value(&webhookPortStr).
				Validate(func(s string) error {
					_, err := strconv.Atoi(s)
					if err != nil {
						return fmt.Errorf("must be a valid number")
					}
					return nil
				}),
			huh.NewInput().
				Title("Webhook Secret").
				Description("A Bearer token for authenticating webhook requests").
				Value(&c.WebhookSecret),
		).WithHideFunc(func() bool {
			return !enableWebhooks
		}),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable Google Drive Sync?").
				Description("Automatically sync a Research folder via rclone").
				Value(&enableSync),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Sync Remote").
				Description("rclone remote path (e.g. gdrive:Research)").
				Value(&c.SyncRemote),
			huh.NewInput().
				Title("Sync Interval (Minutes)").
				Value(&syncIntervalStr).
				Validate(func(s string) error {
					_, err := strconv.Atoi(s)
					if err != nil {
						return fmt.Errorf("must be a valid number")
					}
					return nil
				}),
		).WithHideFunc(func() bool {
			return !enableSync
		}),
		huh.NewGroup(
			huh.NewInput().
				Title("Config Save Path").
				Description("Where should we save this config file?").
				Value(&configPath),
		),
	)

	err := form.Run()
	if err != nil {
		log.Fatalf("Setup aborted: %v", err)
	}

	if enableWebhooks {
		port, _ := strconv.Atoi(webhookPortStr)
		c.WebhookPort = port
	}
	if enableSync {
		interval, _ := strconv.Atoi(syncIntervalStr)
		c.SyncIntervalMinutes = interval
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal config: %v", err)
	}

	os.MkdirAll(filepath.Dir(configPath), 0755)
	err = os.WriteFile(configPath, data, 0600)
	if err != nil {
		log.Fatalf("Failed to write config file to %s: %v", configPath, err)
	}

	log.Infof("Successfully saved config to %s!", configPath)
	log.Infof("You can now run the bot using: ./keybase-obsidian-bot -config=%s", configPath)
	os.Exit(0)
}
