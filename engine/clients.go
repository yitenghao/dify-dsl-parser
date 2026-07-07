package engine

import (
	"context"

	"dify-dsl-parser/dsl"
)

// LLMClient is the abstraction every llm-using runner depends on
// (llm, question-classifier, parameter-extractor, agent).
//
// Implementations adapt this to a real provider (OpenAI, Anthropic, ...).
// A mock implementation is provided in subpackage engine/mock for tests.
type LLMClient interface {
	// Chat performs a single chat completion call. The implementation is
	// responsible for streaming intermediate Delta callbacks if Stream is
	// true; the final assembled text is returned via Result.Text.
	Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error)
}

// LLMRequest is the data a runner hands to LLMClient.Chat.
type LLMRequest struct {
	Provider string
	Model    string
	// Mode is "chat" or "completion".
	Mode string
	// Messages is set in chat mode.
	Messages []ChatMessage
	// Prompt is set in completion mode.
	Prompt string
	// Params is the loose bag of completion parameters from
	// LLMNodeData.Model.CompletionParams (temperature, max_tokens, ...).
	Params map[string]any
	// Stream, when true, asks the implementation to call OnDelta for each
	// streamed chunk. Implementations that don't support streaming may
	// ignore this and call OnDelta once at the end.
	Stream  bool
	OnDelta func(chunk string)
}

// ChatMessage is one entry of LLMRequest.Messages.
type ChatMessage struct {
	Role    string // system | user | assistant
	Content string
}

// LLMResponse is the result of an LLMClient.Chat call.
type LLMResponse struct {
	Text         string
	FinishReason string
	Usage        Usage
}

// Usage tracks token consumption. All fields are best-effort; implementations
// that don't have access to per-call counters can leave them at zero.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// ToolClient is the abstraction for the "tool" node and any other runner
// that calls a Dify plugin / tool.
type ToolClient interface {
	Invoke(ctx context.Context, req ToolRequest) (*ToolResponse, error)
}

// ToolRequest is the data a runner hands to ToolClient.Invoke.
type ToolRequest struct {
	ProviderID     string
	ProviderType   string
	ToolName       string
	Configurations map[string]any
	Parameters     map[string]any
	CredentialID   string
}

// ToolResponse is what the tool returned.
type ToolResponse struct {
	// Result is the typed return value. By convention tools return a
	// dict-shaped object; a single map is fine.
	Result map[string]any
	// Text is an optional human-readable serialization (some tools only
	// produce text, e.g. a search-result blob).
	Text string
}

// ----------------------------------------------------------------------------
// Events (streaming feedback to the caller)
// ----------------------------------------------------------------------------

// Event is the union of all things the engine reports during a Run. The
// Hooks struct on a Run call exposes the emission entry points; runners use
// RunEnv.Emit to push events.
type Event interface{ eventTag() }

// NodeStarted fires when the engine begins executing a node.
type NodeStarted struct {
	NodeID   string
	NodeType dsl.NodeType
}

// NodeFinished fires after a node's runner returns successfully.
type NodeFinished struct {
	NodeID   string
	NodeType dsl.NodeType
	Outputs  map[string]any
	Handle   string // chosen edge source-handle
}

// NodeFailed fires after a runner returns an error or RunResult.Status==failed.
type NodeFailed struct {
	NodeID   string
	NodeType dsl.NodeType
	Error    string
}

// StreamChunk fires for each streamed delta from a node (e.g. LLM tokens or
// answer template rendering).
type StreamChunk struct {
	NodeID string
	Delta  string
}

// WorkflowFinished is the terminal event of a successful run.
type WorkflowFinished struct {
	Outputs map[string]any
}

func (NodeStarted) eventTag()      {}
func (NodeFinished) eventTag()     {}
func (NodeFailed) eventTag()       {}
func (StreamChunk) eventTag()      {}
func (WorkflowFinished) eventTag() {}
