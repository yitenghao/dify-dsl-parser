package engine

import (
	"context"

	"dify-dsl-parser/dsl"
)

// Status describes the terminal state of a single node execution.
//
// Reference: graphon.enums.WorkflowNodeExecutionStatus
type Status string

const (
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusSkipped   Status = "skipped"
)

// DefaultSourceHandle is the implicit source-handle on a non-branching node's
// outgoing edge. Branching nodes (if-else, question-classifier, ...) override
// this in their RunResult.
//
// Reference: graphon.graph.edge.Edge.source_handle
const DefaultSourceHandle = "source"

// RunResult is what a NodeRunner returns after executing a node. The engine
// consumes it to:
//   - write Outputs into the variable pool under the node's id
//   - decide which outgoing edge to follow via EdgeSourceHandle
//   - report status (Status / Error) up to the caller
//
// Reference: graphon.node_events.base.NodeRunResult
type RunResult struct {
	Status           Status
	Inputs           map[string]any
	Outputs          map[string]any
	ProcessData      map[string]any
	EdgeSourceHandle string // empty == DefaultSourceHandle
	Error            string
}

// NodeRunner is the executor for one DSL node type.
//
// Implementations are registered via RegisterRunner. The engine calls Run
// once per occurrence of the node in a graph traversal; runners must be
// stateless across invocations (state belongs in the variable pool).
type NodeRunner interface {
	// Run executes the node. The returned RunResult feeds into both the
	// variable pool (Outputs) and the routing decision (EdgeSourceHandle).
	Run(ctx context.Context, env *RunEnv) (*RunResult, error)
}

// RunEnv is everything a NodeRunner needs to do its job.
//
// It is a per-call carrier; runners must not retain pointers to it across
// invocations.
type RunEnv struct {
	// Node is the dsl.Node currently being executed.
	Node *dsl.Node
	// Pool is the shared variable pool for the current workflow run.
	Pool *VariablePool
	// LLM is the optional LLM client. Runners that need an LLM call
	// (llm, question-classifier, parameter-extractor, agent) will return
	// an error if this is nil.
	LLM LLMClient
	// Tool is the optional tool client.
	Tool ToolClient
	// Emit hands an Event back to the engine, which forwards it to the
	// caller's Hooks. Use this for streaming chunks, status changes, etc.
	Emit func(Event)
}

// runnerFunc is a NodeRunner adapter that lets us register inline funcs.
type runnerFunc func(ctx context.Context, env *RunEnv) (*RunResult, error)

func (f runnerFunc) Run(ctx context.Context, env *RunEnv) (*RunResult, error) {
	return f(ctx, env)
}

// runnerRegistry maps a DSL node type to its runner. RegisterRunner mutates
// this map at init() time only.
var runnerRegistry = map[dsl.NodeType]NodeRunner{}

// RegisterRunner installs runner as the implementation for the given node
// type. Calling it twice with the same type panics: this is a programmer
// error caught at init().
func RegisterRunner(t dsl.NodeType, runner NodeRunner) {
	if _, exists := runnerRegistry[t]; exists {
		panic("engine: duplicate runner registration for type " + t)
	}
	runnerRegistry[t] = runner
}

// LookupRunner returns the runner registered for t, or (nil, false) if none.
func LookupRunner(t dsl.NodeType) (NodeRunner, bool) {
	r, ok := runnerRegistry[t]
	return r, ok
}
