package engine

import (
	"context"
	"fmt"
	"strings"
)

// MockLLM is a deterministic LLMClient useful for tests and offline demos.
//
// You can:
//   - configure a fixed Reply (returned for every call)
//   - configure a per-prompt-substring response map
//   - install a custom Handler for full control
//
// The implementation streams the reply character-by-character through OnDelta
// when Stream is requested, so consumers exercise the streaming code path.
type MockLLM struct {
	// Reply is returned when no other rule matches.
	Reply string
	// Replies maps a prompt substring to a canned response. The first match
	// wins; the substring is searched against the concatenation of all
	// chat messages or the completion prompt.
	Replies map[string]string
	// Handler, if set, takes precedence over Reply / Replies.
	Handler func(ctx context.Context, req LLMRequest) (*LLMResponse, error)
}

// Chat implements LLMClient.
func (m *MockLLM) Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if m.Handler != nil {
		return m.Handler(ctx, req)
	}
	corpus := req.Prompt
	for _, msg := range req.Messages {
		corpus += "\n" + msg.Content
	}
	out := m.Reply
	for needle, reply := range m.Replies {
		if strings.Contains(corpus, needle) {
			out = reply
			break
		}
	}
	if out == "" {
		out = "[mock-llm reply]"
	}
	if req.Stream && req.OnDelta != nil {
		// Stream by word so callers see multiple deltas.
		for _, w := range strings.SplitAfter(out, " ") {
			req.OnDelta(w)
		}
	}
	return &LLMResponse{
		Text:         out,
		FinishReason: "stop",
		Usage: Usage{
			PromptTokens:     len(corpus) / 4,
			CompletionTokens: len(out) / 4,
			TotalTokens:      (len(corpus) + len(out)) / 4,
		},
	}, nil
}

// MockTool is a deterministic ToolClient.
type MockTool struct {
	// Handler, if set, takes full control of the response.
	Handler func(ctx context.Context, req ToolRequest) (*ToolResponse, error)
	// Results maps a "<provider_id>/<tool_name>" key to a canned response.
	Results map[string]ToolResponse
}

// Invoke implements ToolClient.
func (m *MockTool) Invoke(ctx context.Context, req ToolRequest) (*ToolResponse, error) {
	if m.Handler != nil {
		return m.Handler(ctx, req)
	}
	key := fmt.Sprintf("%s/%s", req.ProviderID, req.ToolName)
	if r, ok := m.Results[key]; ok {
		return &r, nil
	}
	return &ToolResponse{
		Result: map[string]any{
			"echo": req.Parameters,
		},
		Text: fmt.Sprintf("[mock-tool %s]", key),
	}, nil
}
