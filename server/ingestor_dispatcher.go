/*
 * Copyright (c) 2026 Lyndon Washington
 * Licensed under the MIT License. See LICENSE in the project root for license information.
 */

package server

import (
	"context"
	"fmt"

	"charm.land/log/v2"
)

// SimplexSender is the subset of the simplex.Client interface required by IngestorDispatcher.
type SimplexSender interface {
	Send(address string, message string) error
}

// IngestorDispatcher implements MessageDispatcher for the ingestor role.
// It relays every received payload to the Executor over SimpleX by connecting
// to the configured executor address and delivering the message over an
// end-to-end encrypted SimpleX channel.
type IngestorDispatcher struct {
	SimplexClient   SimplexSender
	ExecutorAddress string
}

// NewIngestorDispatcher creates a new IngestorDispatcher.
func NewIngestorDispatcher(client SimplexSender, executorAddress string) *IngestorDispatcher {
	return &IngestorDispatcher{
		SimplexClient:   client,
		ExecutorAddress: executorAddress,
	}
}

// Handle forwards msg to the configured Executor over SimpleX.
func (d *IngestorDispatcher) Handle(_ context.Context, msg string) error {
	if d.ExecutorAddress == "" {
		return fmt.Errorf("ingestor: executor address is not configured")
	}
	log.Infof("ingestor: forwarding payload to executor (%s)", d.ExecutorAddress)
	if err := d.SimplexClient.Send(d.ExecutorAddress, msg); err != nil {
		return fmt.Errorf("ingestor: forward via SimpleX: %w", err)
	}
	return nil
}
