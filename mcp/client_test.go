/*
 * Copyright (c) 2026 Lyndon Washington
 * Licensed under the MIT License. See LICENSE in the project root for license information.
 */

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helpers for constructing JSON-RPC responses ---

func makeResponse(id uint64, result any) string {
	r, _ := json.Marshal(result)
	msg := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Result:  json.RawMessage(r),
	}
	b, _ := json.Marshal(msg)
	return string(b) + "\n"
}

func makeErrorResponse(id uint64, code int, message string) string {
	msg := JSONRPCMessage{
		JSONRPC: "2.0",
		ID:      &id,
		Error:   &JSONRPCError{Code: code, Message: message},
	}
	b, _ := json.Marshal(msg)
	return string(b) + "\n"
}

func makeToolResult(text string, isError bool) any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": isError,
	}
}

// --- Fake MCP server over in-memory pipes ---

// fakeServer simulates an MCP server: it responds to the initialize request
// and then to tools/call requests with a pre-configured response.
type fakeServer struct {
	// toolResponse is returned for any tools/call request.
	toolResponse string
	toolIsError  bool
}

// serve reads JSON-RPC requests from r and writes responses to w.
// It handles exactly two requests: initialize, then one tools/call.
func (fs *fakeServer) serve(r io.Reader, w io.Writer) {
	dec := json.NewDecoder(r)
	for {
		var req JSONRPCMessage
		if err := dec.Decode(&req); err != nil {
			return
		}
		if req.ID == nil {
			// notification — no response needed
			continue
		}
		switch req.Method {
		case "initialize":
			resp := makeResponse(*req.ID, map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]any{},
				"serverInfo":      map[string]any{"name": "fake-mcp", "version": "0.0.1"},
			})
			fmt.Fprint(w, resp) //nolint:errcheck // in-memory pipe; write errors are impossible in tests
		case "tools/call":
			resp := makeResponse(*req.ID, makeToolResult(fs.toolResponse, fs.toolIsError))
			fmt.Fprint(w, resp) //nolint:errcheck // in-memory pipe; write errors are impossible in tests
		}
	}
}

// newTestClient builds a Client wired to an in-memory fake server.
func newTestClient(t *testing.T, fs *fakeServer) *Client {
	t.Helper()
	// Client stdin → server reads from serverIn
	serverInR, serverInW := io.Pipe()
	// Server writes to serverOut → client reads from clientOut
	serverOutR, serverOutW := io.Pipe()

	go func() {
		fs.serve(serverInR, serverOutW)
	}()

	c := &Client{
		stdin:  serverInW,
		stdout: serverOutR,
		done:   make(chan struct{}),
	}
	go c.readLoop()

	// Perform the MCP handshake so the client is ready.
	initParams := map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "test", "version": "0.0.1"},
		"capabilities":    map[string]any{},
	}
	_, err := c.Call(context.Background(), "initialize", initParams)
	require.NoError(t, err)
	return c
}

// --- Tests ---

func TestCallTool_SuccessResponse(t *testing.T) {
	fs := &fakeServer{toolResponse: `{"content": "note", "fm": null}`, toolIsError: false}
	c := newTestClient(t, fs)

	result, err := c.CallTool(context.Background(), "read_note", map[string]any{"path": "Inbox.md"})
	require.NoError(t, err)
	assert.NotEmpty(t, result)
}

func TestCallTool_IsErrorFlagReturnsError(t *testing.T) {
	fs := &fakeServer{toolResponse: "file not found", toolIsError: true}
	c := newTestClient(t, fs)

	_, err := c.CallTool(context.Background(), "read_note", map[string]any{"path": "Missing.md"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file not found")
}

func TestCallTool_JSONRPCLevelError(t *testing.T) {
	// Serve an error at the JSON-RPC level (not isError flag).
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()

	go func() {
		dec := json.NewDecoder(serverInR)
		for {
			var req JSONRPCMessage
			if err := dec.Decode(&req); err != nil {
				return
			}
			if req.ID == nil {
				continue
			}
			if req.Method == "initialize" {
				fmt.Fprint(serverOutW, makeResponse(*req.ID, map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}})) //nolint:errcheck
			} else {
				fmt.Fprint(serverOutW, makeErrorResponse(*req.ID, -32601, "method not found")) //nolint:errcheck
			}
		}
	}()

	c := &Client{stdin: serverInW, stdout: serverOutR, done: make(chan struct{})}
	go c.readLoop()

	_, err := c.Call(context.Background(), "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "test", "version": "0.0.1"},
		"capabilities":    map[string]any{},
	})
	require.NoError(t, err)

	_, err = c.CallTool(context.Background(), "nonexistent_tool", map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "method not found")
}

func TestNotify_DoesNotExpectResponse(t *testing.T) {
	fs := &fakeServer{toolResponse: "ok", toolIsError: false}
	c := newTestClient(t, fs)

	// Notify should return immediately without blocking for a response.
	err := c.Notify("notifications/initialized", map[string]any{})
	assert.NoError(t, err)
}

func TestCallTool_ContextCancellation(t *testing.T) {
	// Build a server that never responds to tools/call.
	serverInR, serverInW := io.Pipe()
	serverOutR, serverOutW := io.Pipe()

	go func() {
		dec := json.NewDecoder(serverInR)
		for {
			var req JSONRPCMessage
			if err := dec.Decode(&req); err != nil {
				return
			}
			if req.ID == nil {
				continue
			}
			if req.Method == "initialize" {
				fmt.Fprint(serverOutW, makeResponse(*req.ID, map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}})) //nolint:errcheck
			}
			// tools/call: deliberately no response → caller should timeout via ctx.
		}
	}()

	c := &Client{stdin: serverInW, stdout: serverOutR, done: make(chan struct{})}
	go c.readLoop()

	_, err := c.Call(context.Background(), "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "test", "version": "0.0.1"},
		"capabilities":    map[string]any{},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = c.CallTool(ctx, "write_note", map[string]any{"path": "X.md", "content": "hi"})
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "context canceled") || strings.Contains(err.Error(), "context deadline exceeded"),
		"expected context error, got: %v", err)
}
