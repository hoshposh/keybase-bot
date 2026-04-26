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

func runSetup(initialConfigPath string) {
	var c Config
	var enableWebhooks bool
	var enableSync bool

	homeDir, _ := os.UserHomeDir()
	defaultConfigPath := filepath.Join(homeDir, ".config", "keybase-bot", "config.json")
	var configPath = defaultConfigPath

	if initialConfigPath != "" {
		configPath = initialConfigPath
	}

	// Try to load defaults if the file already exists
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &c); err == nil {
			log.Infof("Pre-filling wizard with existing config from %s", configPath)
			if c.Role == "" {
				c.Role = "standalone"
			}
			// basic heuristic to determine if we should turn on the confims
			if c.WebhookPort != 0 || c.WebhookSecret != "" {
				enableWebhooks = true
			}
			if c.SyncRemote != "" || c.SyncIntervalMinutes != 0 {
				enableSync = true
			}
		}
	} else {
		c.Role = "standalone"
	}

	var webhookPortStr = "8080"
	if c.WebhookPort != 0 {
		webhookPortStr = strconv.Itoa(c.WebhookPort)
	}
	var syncIntervalStr = "15"
	if c.SyncIntervalMinutes != 0 {
		syncIntervalStr = strconv.Itoa(c.SyncIntervalMinutes)
	}

	log.Print("Welcome to the Keybase Obsidian Bot Setup Wizard!")

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Deployment Role").
				Description("Select how this bot instance will run").
				Options(
					huh.NewOption("Standalone (Both Webhooks + Executor)", "standalone"),
					huh.NewOption("Cloud Ingestor (Webhooks to Keybase)", "ingestor"),
					huh.NewOption("Local Executor (Keybase to Vault)", "executor"),
				).
				Value(&c.Role),
		),
		huh.NewGroup(
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
		),
		huh.NewGroup(
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
		).WithHideFunc(func() bool {
			return c.Role == "executor"
		}),
		huh.NewGroup(
			huh.NewInput().
				Title("Job Channel").
				Description("Private Keybase team channel for internal jobs (e.g. 'myteam.jobs')").
				Value(&c.JobChannel).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("job channel cannot be empty")
					}
					return nil
				}),
		).WithHideFunc(func() bool {
			return c.Role == "standalone"
		}),
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
		).WithHideFunc(func() bool {
			return c.Role == "ingestor"
		}),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable Webhooks?").
				Description("This allows tools like Feedly to send links to your vault").
				Value(&enableWebhooks),
		).WithHideFunc(func() bool {
			return c.Role == "executor" // Executors don't do webhooks
		}),
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
			return c.Role == "executor" || !enableWebhooks
		}),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Enable Google Drive Sync?").
				Description("Automatically sync a Research folder via rclone").
				Value(&enableSync),
		).WithHideFunc(func() bool {
			return c.Role == "ingestor" // Ingestors don't do sync or vault
		}),
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
			return c.Role == "ingestor" || !enableSync
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
