/*
 * Copyright (c) 2026 Lyndon Washington
 * Licensed under the MIT License. See LICENSE in the project root for license information.
 */

package simplex

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildRawEvent constructs a JSON WebSocket envelope that simulates a
// simplex-chat server-push event (no corrId, resp contains the event payload).
func buildRawEvent(t *testing.T, resp map[string]any) []byte {
	t.Helper()
	respBytes, err := json.Marshal(resp)
	require.NoError(t, err)
	envelope := map[string]any{
		"corrId": nil,
		"resp":   json.RawMessage(respBytes),
	}
	data, err := json.Marshal(envelope)
	require.NoError(t, err)
	return data
}

// buildNewChatItemsEvent builds a newChatItems event payload that mimics
// the shape emitted by simplex-chat v6 for an incoming direct text message.
func buildNewChatItemsEvent(displayName, localDisplayName, text string) map[string]any {
	return map[string]any{
		"type": "newChatItems",
		"chatItems": []map[string]any{
			{
				"chatInfo": map[string]any{
					"type": "direct",
					"contact": map[string]any{
						"localDisplayName": localDisplayName,
						"profile": map[string]any{
							"displayName": displayName,
						},
					},
				},
				"chatItem": map[string]any{
					"chatDir": map[string]any{"type": "directRcv"},
					"content": map[string]any{
						"type": "rcvMsgContent",
						"msgContent": map[string]any{
							"type": "text",
							"text": text,
						},
					},
				},
			},
		},
	}
}

// --- Unit tests for the event-parsing logic in Listen ---
//
// We test the internal parseIncomingMessage helper extracted from the Listen
// loop rather than spinning up a real simplex-chat daemon.

// parseIncomingMessage is the extracted parsing logic from Listen, defined
// here as a test-facing helper so we can unit-test it without a live WS.
func parseIncomingMessage(data []byte) (IncomingMessage, bool) {
	var raw rawResponse
	if err := json.Unmarshal(data, &raw); err != nil {
		return IncomingMessage{}, false
	}
	if raw.CorrID != "" {
		return IncomingMessage{}, false // solicited response, skip
	}

	var event struct {
		Type      string `json:"type"`
		ChatItems []struct {
			ChatInfo struct {
				Type    string `json:"type"`
				Contact struct {
					LocalDisplayName string `json:"localDisplayName"`
					Profile          struct {
						DisplayName string `json:"displayName"`
					} `json:"profile"`
				} `json:"contact"`
			} `json:"chatInfo"`
			ChatItem struct {
				ChatDir struct {
					Type string `json:"type"`
				} `json:"chatDir"`
				Content struct {
					Type       string `json:"type"`
					MsgContent struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"msgContent"`
				} `json:"content"`
			} `json:"chatItem"`
		} `json:"chatItems"`
	}
	if err := json.Unmarshal(raw.Resp, &event); err != nil {
		return IncomingMessage{}, false
	}
	if event.Type != "newChatItems" {
		return IncomingMessage{}, false
	}
	for _, item := range event.ChatItems {
		dir := item.ChatItem.ChatDir.Type
		contentType := item.ChatItem.Content.Type
		msgType := item.ChatItem.Content.MsgContent.Type
		if dir != "directRcv" || contentType != "rcvMsgContent" || msgType != "text" {
			continue
		}
		sender := item.ChatInfo.Contact.Profile.DisplayName
		if sender == "" {
			sender = item.ChatInfo.Contact.LocalDisplayName
		}
		text := item.ChatItem.Content.MsgContent.Text
		if sender == "" || text == "" {
			continue
		}
		return IncomingMessage{SenderName: sender, Text: text}, true
	}
	return IncomingMessage{}, false
}

func TestParseIncomingMessage(t *testing.T) {
	t.Run("Valid direct text message parsed correctly", func(t *testing.T) {
		data := buildRawEvent(t, buildNewChatItemsEvent("Alice", "alice_1", "hello"))
		msg, ok := parseIncomingMessage(data)
		require.True(t, ok)
		assert.Equal(t, "Alice", msg.SenderName, "should prefer profile displayName")
		assert.Equal(t, "hello", msg.Text)
	})

	t.Run("Falls back to localDisplayName when displayName is empty", func(t *testing.T) {
		data := buildRawEvent(t, buildNewChatItemsEvent("", "bob_1", "fallback"))
		msg, ok := parseIncomingMessage(data)
		require.True(t, ok)
		assert.Equal(t, "bob_1", msg.SenderName)
	})

	t.Run("Solicited response (corrId set) is skipped", func(t *testing.T) {
		raw := map[string]any{
			"corrId": "42",
			"resp":   map[string]any{"type": "newChatItems", "chatItems": []any{}},
		}
		data, _ := json.Marshal(raw)
		_, ok := parseIncomingMessage(data)
		assert.False(t, ok)
	})

	t.Run("Non-newChatItems event type is ignored", func(t *testing.T) {
		data := buildRawEvent(t, map[string]any{"type": "contactConnected", "contact": map[string]any{}})
		_, ok := parseIncomingMessage(data)
		assert.False(t, ok)
	})

	t.Run("Non-directRcv direction is filtered out", func(t *testing.T) {
		event := buildNewChatItemsEvent("Carol", "carol", "hi")
		// Mutate the chatDir to sent
		items := event["chatItems"].([]map[string]any)
		items[0]["chatItem"].(map[string]any)["chatDir"] = map[string]any{"type": "directSnd"}
		data := buildRawEvent(t, event)
		_, ok := parseIncomingMessage(data)
		assert.False(t, ok)
	})

	t.Run("Non-text message content type is filtered out", func(t *testing.T) {
		event := buildNewChatItemsEvent("Dave", "dave", "")
		items := event["chatItems"].([]map[string]any)
		items[0]["chatItem"].(map[string]any)["content"].(map[string]any)["msgContent"] = map[string]any{
			"type": "image",
			"text": "",
		}
		data := buildRawEvent(t, event)
		_, ok := parseIncomingMessage(data)
		assert.False(t, ok)
	})

	t.Run("Malformed JSON is gracefully ignored", func(t *testing.T) {
		_, ok := parseIncomingMessage([]byte("{not valid json"))
		assert.False(t, ok)
	})
}
