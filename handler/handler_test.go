package handler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockMCPClient struct {
	calledName string
	calledArgs map[string]interface{}
}

func (m *mockMCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) ([]byte, error) {
	m.calledName = name
	m.calledArgs = args
	return []byte("success"), nil
}

func TestHandle(t *testing.T) {
	mockTime := func() time.Time {
		return time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)
	}
	expectedHeading := "\n## 2026-04-06\n\n"

	t.Run("Note prefix", func(t *testing.T) {
		m := &mockMCPClient{}
		h := NewMessageHandler("/vault", m)
		h.Now = mockTime

		err := h.Handle(context.Background(), "!note Hello World")
		require.NoError(t, err)

		assert.Equal(t, "write_note", m.calledName)
		assert.Equal(t, "Inbox.md", m.calledArgs["path"])
		assert.Equal(t, expectedHeading+"Hello World\n", m.calledArgs["content"])
	})

	t.Run("Todo prefix", func(t *testing.T) {
		m := &mockMCPClient{}
		h := NewMessageHandler("/vault", m)
		h.Now = mockTime

		err := h.Handle(context.Background(), "!todo Buy milk")
		require.NoError(t, err)

		assert.Equal(t, "write_note", m.calledName)
		assert.Equal(t, "Tasks.md", m.calledArgs["path"])
		assert.Equal(t, expectedHeading+"- [ ] Buy milk\n", m.calledArgs["content"])
	})

	t.Run("Link prefix", func(t *testing.T) {
		m := &mockMCPClient{}
		h := NewMessageHandler("/vault", m)
		h.Now = mockTime

		err := h.Handle(context.Background(), "!link https://example.com")
		require.NoError(t, err)

		assert.Equal(t, "write_note", m.calledName)
		assert.Equal(t, "Links.md", m.calledArgs["path"])
		assert.Equal(t, expectedHeading+" - <https://example.com>\n", m.calledArgs["content"])
	})

	t.Run("Automatic link detection", func(t *testing.T) {
		m := &mockMCPClient{}
		h := NewMessageHandler("/vault", m)
		h.Now = mockTime

		err := h.Handle(context.Background(), "https://google.com")
		require.NoError(t, err)

		assert.Equal(t, "write_note", m.calledName)
		assert.Equal(t, "Links.md", m.calledArgs["path"])
		assert.Equal(t, expectedHeading+" - <https://google.com>\n", m.calledArgs["content"])
	})

	t.Run("No prefix", func(t *testing.T) {
		m := &mockMCPClient{}
		h := NewMessageHandler("/vault", m)
		h.Now = mockTime

		err := h.Handle(context.Background(), "Just a random thought")
		require.NoError(t, err)

		assert.Equal(t, "write_note", m.calledName)
		assert.Equal(t, "Daily/2026-04-06.md", m.calledArgs["path"])
		assert.Equal(t, "Just a random thought\n", m.calledArgs["content"])
	})

	t.Run("Empty message", func(t *testing.T) {
		m := &mockMCPClient{}
		h := NewMessageHandler("/vault", m)

		err := h.Handle(context.Background(), "   ")
		require.NoError(t, err)

		assert.Empty(t, m.calledName)
	})
}
