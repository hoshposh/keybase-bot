/*
 * Copyright (c) 2026 Lyndon Washington
 * Licensed under the MIT License. See LICENSE in the project root for license information.
 */

package sync

import (
	"context"
	"os/exec"
	"path/filepath"
	"time"

	"charm.land/log/v2"
)

// DriveSync manages periodic synchronization of a local directory to a remote via rclone.
type DriveSync struct {
	VaultPath string
	Interval  time.Duration
	Remote    string // e.g., "gdrive:Research"
}

// NewDriveSync creates a new DriveSync instance.
func NewDriveSync(vaultPath, remote string, interval time.Duration) *DriveSync {
	return &DriveSync{
		VaultPath: vaultPath,
		Remote:    remote,
		Interval:  interval,
	}
}

// Start begins the synchronization loop. It blocks until the context is canceled.
func (s *DriveSync) Start(ctx context.Context) {
	if s.Remote == "" {
		log.Print("Drive sync remote is empty, sync loop disabled.")
		return
	}

	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()

	log.Printf("Starting Google Drive sync loop for Research directory (interval: %v)", s.Interval)

	// Run an initial sync in a non-blocking way
	go s.performSync(ctx)

	for {
		select {
		case <-ticker.C:
			go s.performSync(ctx)
		case <-ctx.Done():
			log.Print("Stopping Google Drive sync loop")
			return
		}
	}
}

func (s *DriveSync) performSync(ctx context.Context) {
	researchDir := filepath.Join(s.VaultPath, "Research")
	log.Printf("Executing rclone sync from %s to %s...", researchDir, s.Remote)

	// Command: rclone sync /path/to/Research remote
	// Adding --create-empty-src-dirs to ensure correct replication
	cmd := exec.CommandContext(ctx, "rclone", "sync", researchDir, s.Remote, "--create-empty-src-dirs") //nolint:gosec // fixed binary; args are config values

	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("rclone sync failed: %v\nOutput: %s", err, string(output))
	} else {
		log.Printf("rclone sync successful.")
	}
}
