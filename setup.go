/*
 * Copyright (c) 2026 Lyndon Washington
 * Licensed under the MIT License. See LICENSE in the project root for license information.
 */

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"charm.land/log/v2"
	"charm.land/huh/v2"
)

func runSetup(initialConfigPath string) {
	var c Config
	var enableWebhooks bool
	var enableSync bool
	var autoProvision bool
	var teamName string
	var channelName string

	keybaseInstalled := false
	if _, err := exec.LookPath("keybase"); err == nil {
		keybaseInstalled = true
	}

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

	if c.JobChannel != "" {
		parts := strings.SplitN(c.JobChannel, ".", 2)
		teamName = parts[0]
		if len(parts) > 1 {
			channelName = parts[1]
		}
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
			huh.NewConfirm().
				Title("Auto-Provision Keybase Bots & Infrastructure?").
				Description("I can automatically create your bots and channels using the local Keybase CLI.").
				Value(&autoProvision),
		).WithHideFunc(func() bool { return !keybaseInstalled }),
		huh.NewGroup(
			huh.NewInput().
				Title("Keybase Bot Username").
				Description("Username for the Keybase bot (2-15 chars, alphanumeric or underscores)").
				Value(&c.BotUsername).
				Validate(func(s string) error {
					if len(s) < 2 || len(s) > 15 {
						return fmt.Errorf("username must be between 2 and 15 characters")
					}
					if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(s) {
						return fmt.Errorf("username can only contain letters, numbers, and underscores")
					}
					return nil
				}),
			huh.NewInput().
				TitleFunc(func() string {
					if autoProvision {
						return "Keybase Paper Key Directory"
					}
					return "Keybase Paper Key Path"
				}, &autoProvision).
				DescriptionFunc(func() string {
					if autoProvision {
						return "Directory to save the new paper key (e.g. /home/user/)"
					}
					return "Path to a file containing your existing bot's paper key"
				}, &autoProvision).
				Value(&c.SecretPath).
				Validate(func(s string) error {
					if s == "" {
						if autoProvision {
							return fmt.Errorf("directory path cannot be empty")
						}
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
				Title("Keybase Team Name").
				Description("Keybase team for internal jobs (e.g. 'my_automation')").
				Value(&teamName).
				Validate(func(s string) error {
					if len(s) < 2 || len(s) > 16 {
						return fmt.Errorf("team name must be between 2 and 16 characters")
					}
					return nil
				}),
			huh.NewInput().
				Title("Keybase Channel Name").
				Description("Channel within the team (e.g. 'vault-ingress')").
				Value(&channelName).
				Validate(func(s string) error {
					if s == "" || len(s) > 20 {
						return fmt.Errorf("channel name must be between 1 and 20 characters")
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

	if c.Role == "ingestor" || c.Role == "executor" {
		c.JobChannel = teamName
		if channelName != "" {
			c.JobChannel += "." + channelName
		}
	}

	if autoProvision {
		c.SecretPath = filepath.Join(c.SecretPath, c.BotUsername+"_paper_key.txt")
		log.Infof("Auto-provisioning Keybase bots and infrastructure...")
		err := provisionKeybaseBot(c.Role, c.BotUsername, c.SecretPath, c.JobChannel)
		if err != nil {
			log.Fatalf("Failed to provision bot: %v", err)
		}
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

func provisionKeybaseBot(role, botUsername, secretPath, jobChannel string) error {
	log.Infof("Generating bot token...")
	tokenCmd := exec.Command("keybase", "bot", "token", "create")
	tokenOut, err := tokenCmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			log.Errorf("Token generation failed: %s", string(exitError.Stderr))
		}
		return fmt.Errorf("failed to create bot token: %v", err)
	}
	token := strings.TrimSpace(string(tokenOut))

	tmpHome, err := os.MkdirTemp("", "kb_bot_setup_*")
	if err != nil {
		return fmt.Errorf("failed to create temp home directory: %v", err)
	}
	defer os.RemoveAll(tmpHome)

	log.Infof("Signing up bot account @%s...", botUsername)
	signupCmd := exec.Command("keybase", "--home="+tmpHome, "--standalone", "bot", "signup", "-u", botUsername, "-t", token)
	signupOut, err := signupCmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			log.Errorf("Signup failed: %s", string(exitError.Stderr))
		}
		return fmt.Errorf("bot signup failed: %v", err)
	}

	paperKey := strings.TrimSpace(string(signupOut))

	log.Infof("Saving paper key to %s...", secretPath)
	os.MkdirAll(filepath.Dir(secretPath), 0755)
	if err := os.WriteFile(secretPath, []byte(paperKey), 0600); err != nil {
		return fmt.Errorf("failed to write paper key: %v", err)
	}

	if role == "ingestor" || role == "executor" {
		parts := strings.SplitN(jobChannel, ".", 2)
		teamName := parts[0]
		channelStr := ""
		if len(parts) > 1 {
			channelStr = parts[1]
		} else {
			channelStr = "general"
		}

		log.Infof("Ensuring team %s exists...", teamName)
		checkCmd := exec.Command("keybase", "team", "list-members", teamName)
		if err := checkCmd.Run(); err != nil {
			log.Infof("Team %s does not appear to exist. Creating...", teamName)
			createCmd := exec.Command("keybase", "team", "create", teamName)
			if out, createErr := createCmd.CombinedOutput(); createErr != nil {
				log.Warnf("Could not create team: %v (output: %s). Assuming it exists or skipping.", createErr, string(out))
			}
		}

		kbRole := "bot"
		if role == "executor" {
			kbRole = "writer"
		}

		log.Infof("Adding bot to team %s with role %s...", teamName, kbRole)
		addCmd := exec.Command("keybase", "team", "add-member", teamName, "--user", botUsername, "--role", kbRole)
		if out, addErr := addCmd.CombinedOutput(); addErr != nil {
			log.Warnf("Failed to add member to team: %v (output: %s). It may already be a member.", addErr, string(out))
		}

		if channelStr != "general" {
			log.Infof("Ensuring channel #%s exists...", channelStr)
			chatCmd := exec.Command("keybase", "chat", "send", "--channel", "#"+channelStr, teamName, "Initializing bot channel...")
			if out, chatErr := chatCmd.CombinedOutput(); chatErr != nil {
				log.Warnf("Failed to send init message to channel: %v (output: %s)", chatErr, string(out))
			}
		}
	}

	log.Infof("Bot auto-provisioning completed successfully!")
	return nil
}
