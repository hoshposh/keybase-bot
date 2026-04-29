/*
 * Copyright (c) 2026 Lyndon Washington
 * Licensed under the MIT License. See LICENSE in the project root for license information.
 */

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"

	"charm.land/log/v2"
)

// Client is a minimal JSON-RPC client over stdio for communicating with an MCP server
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser

	reqMap sync.Map
	reqID  atomic.Uint64

	closeOnce sync.Once
	done      chan struct{}
}

// JSONRPCMessage is a generic struct for JSON-RPC messages
type JSONRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *uint64         `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type pendingRequest struct {
	ch chan *JSONRPCMessage
}

// NewClient starts an MCP server process and creates a Client.
// serverCmd is the command to run, e.g., []string{"npx", "-y", "@bitbonsai/mcpvault", "--vault", "/path/to/vault"}
func NewClient(ctx context.Context, serverCmd []string, vaultPath string) (*Client, error) {
	if len(serverCmd) == 0 {
		return nil, fmt.Errorf("empty server command")
	}

	args := serverCmd[1:]
	if vaultPath != "" {
		args = append(args, vaultPath)
	}
	cmd := exec.CommandContext(ctx, serverCmd[0], args...) //nolint:gosec // serverCmd is from validated user config
	cmd.Env = append(os.Environ(), "OBSIDIAN_VAULT_PATH="+vaultPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	// MCP servers often print logs to stderr, let's capture them to our standard logger
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start mcp server process: %w", err)
	}

	go streamLogs(stderr, "mcp-server")

	c := &Client{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		done:   make(chan struct{}),
	}

	go c.readLoop()

	// Initializing MCP usually requires an "initialize" request.
	initParams := map[string]any{
		"protocolVersion": "2024-11-05", // Standard MCP protocol version
		"clientInfo": map[string]string{
			"name":    "umbilical",
			"version": "1.0.0",
		},
		"capabilities": map[string]any{},
	}

	if _, err := c.Call(ctx, "initialize", initParams); err != nil {
		_ = c.Close() // best-effort cleanup; return the init error
		return nil, fmt.Errorf("MCP initialize failed: %w", err)
	}

	// Followed by notifications/initialized
	if err := c.Notify("notifications/initialized", map[string]any{}); err != nil {
		log.Printf("Warning: failed to send initialized notification: %v", err)
	}

	return c, nil
}

func streamLogs(r io.Reader, prefix string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		log.Printf("[%s] %s", prefix, scanner.Text())
	}
}

func (c *Client) readLoop() {
	scanner := bufio.NewScanner(c.stdout)
	// We might need to increase buffer size for large responses
	const maxCapacity = 10 * 1024 * 1024
	buf := make([]byte, maxCapacity)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		data := scanner.Bytes()

		var msg JSONRPCMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("JSON-RPC parse error: %v. Raw data: %s", err, string(data))
			continue
		}

		if msg.ID != nil {
			// It's a response
			ch, ok := c.reqMap.Load(*msg.ID)
			if ok {
				req := ch.(*pendingRequest)
				req.ch <- &msg
				c.reqMap.Delete(*msg.ID)
			}
		}
		// server notifications (no ID) are intentionally ignored
	}

	if err := scanner.Err(); err != nil {
		log.Printf("MCP stdout Read error: %v", err)
	}
	close(c.done)
}

func (c *Client) Call(ctx context.Context, method string, params any) (*JSONRPCMessage, error) {
	id := c.reqID.Add(1)

	pBytes, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}

	req := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  method,
		Params:  pBytes,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	reqBytes = append(reqBytes, '\n')

	pending := &pendingRequest{ch: make(chan *JSONRPCMessage, 1)}
	c.reqMap.Store(id, pending)

	defer c.reqMap.Delete(id)

	if _, err := c.stdin.Write(reqBytes); err != nil {
		return nil, fmt.Errorf("write error: %w", err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.done:
		return nil, fmt.Errorf("mcp client closed")
	case resp := <-pending.ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("JSON-RPC error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp, nil
	}
}

func (c *Client) Notify(method string, params any) error {
	pBytes, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal params: %w", err)
	}

	req := JSONRPCMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  pBytes,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}
	reqBytes = append(reqBytes, '\n')

	if _, err := c.stdin.Write(reqBytes); err != nil {
		return fmt.Errorf("write error: %w", err)
	}
	return nil
}

// CallTool invokes tools/call on the MCP server
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) ([]byte, error) {
	params := map[string]any{
		"name":      name,
		"arguments": args,
	}

	resp, err := c.Call(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}

	// resp.Result should be a CallToolResult
	// {"content": [{"type": "text", "text": "result"}], "isError": false}
	var result struct {
		IsError bool `json:"isError"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return resp.Result, nil // Return raw if doesn't match standard tool result format
	}

	// MCP officially uses the isError flag to indicate tool failure without throwing a JSON-RPC level error.
	if result.IsError {
		errMsg := "MCP Tool returned an error"
		if len(result.Content) > 0 {
			errMsg = result.Content[0].Text
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	if len(result.Content) > 0 {
		return []byte(result.Content[0].Text), nil
	}

	return resp.Result, nil
}

func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		err = c.stdin.Close()
		if c.cmd != nil && c.cmd.Process != nil {
			if killErr := c.cmd.Process.Kill(); killErr != nil && err == nil {
				err = killErr
			}
		}
	})
	return err
}
