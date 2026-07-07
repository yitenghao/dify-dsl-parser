package engine

import (
	"context"
	"errors"
	"fmt"

	"dify-dsl-parser/dsl"
)

// Hooks lets callers observe a workflow run as it happens. Every field is
// optional; nil hooks are silently ignored.
type Hooks struct {
	OnEvent func(Event)
}

// Engine is the driver that executes a parsed DSL.
//
// Construct one per workflow run; the variable pool inside is mutated as the
// run progresses, so concurrent reuse across runs requires a fresh Engine
// (or at least a fresh VariablePool).
type Engine struct {
	dsl   *dsl.DSL
	pool  *VariablePool
	llm   LLMClient
	tool  ToolClient
	hooks Hooks

	// MaxSteps caps how many node executions one Run can perform. This
	// guards against accidental infinite loops in user-built graphs.
	// Default = 1000.
	MaxSteps int
}

// New builds an Engine for the given DSL.
//
// The returned Engine has an empty variable pool; seed system / env /
// conversation variables before calling Run.
func New(d *dsl.DSL) *Engine {
	return &Engine{
		dsl:      d,
		pool:     NewVariablePool(),
		MaxSteps: 1000,
	}
}

// Pool returns the variable pool. Callers seed inputs via Pool().SetSystem etc.
// before Run, and read final results from it after Run returns.
func (e *Engine) Pool() *VariablePool { return e.pool }

// WithLLM injects an LLMClient implementation. Required for llm /
// question-classifier / parameter-extractor / agent nodes; without it
// those runners return an error at execution time.
func (e *Engine) WithLLM(c LLMClient) *Engine { e.llm = c; return e }

// WithTool injects a ToolClient implementation.
func (e *Engine) WithTool(c ToolClient) *Engine { e.tool = c; return e }

// WithHooks installs runtime callbacks (per-event observers).
func (e *Engine) WithHooks(h Hooks) *Engine { e.hooks = h; return e }

// RunInput is the per-run input bundle. All fields are optional.
type RunInput struct {
	// UserInputs is the form data submitted by the user. Each entry is
	// added to the variable pool under both "sys.<name>" (for compatibility
	// with templates referencing sys.*) and the start node's id (for
	// {{#start.<name>#}} references).
	UserInputs map[string]any
	// Query is shorthand for UserInputs["query"] and sys.query.
	Query string
	// Files is shorthand for sys.files.
	Files []any
	// UserID populates sys.user_id.
	UserID string
	// ConversationID populates sys.conversation_id.
	ConversationID string
}

// Result is the terminal state of a Run.
type Result struct {
	// Outputs is the value returned by the terminal node (end / answer).
	Outputs map[string]any
	// FinalNode is the ID of the terminal node that produced Outputs.
	FinalNode string
	// Steps is how many nodes were executed.
	Steps int
}

// Run executes the workflow synchronously. The single goroutine traverses
// the graph following the routing table; each node's runner is invoked in
// turn; the call returns when a RESPONSE-type node (end / answer) finishes
// or when a runner returns an error.
//
// This mirrors the simplified single-threaded slice of graphon's
// queue-based engine, which it ultimately collapses to when there's no
// parallelism to exploit.
func (e *Engine) Run(ctx context.Context, in RunInput) (*Result, error) {
	if e.dsl == nil || !e.dsl.IsWorkflow() {
		return nil, errors.New("engine: not a workflow DSL")
	}
	g := e.dsl.Workflow.Graph

	// Seed the pool with system variables and user inputs.
	e.seedInputs(in, g)

	startNode, err := findRoot(g)
	if err != nil {
		return nil, err
	}

	routing := buildRoutingTable(g)

	emit := func(ev Event) {
		if e.hooks.OnEvent != nil {
			e.hooks.OnEvent(ev)
		}
	}

	cur := startNode
	steps := 0
	maxSteps := e.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 1000
	}

	for cur != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		steps++
		if steps > maxSteps {
			return nil, fmt.Errorf("engine: exceeded MaxSteps=%d", maxSteps)
		}

		runner, ok := LookupRunner(cur.Data.NodeType())
		if !ok {
			return nil, fmt.Errorf("engine: no runner for node type %q (id=%s)",
				cur.Data.NodeType(), cur.ID)
		}

		emit(NodeStarted{NodeID: cur.ID, NodeType: cur.Data.NodeType()})

		env := &RunEnv{
			Node: cur,
			Pool: e.pool,
			LLM:  e.llm,
			Tool: e.tool,
			Emit: emit,
		}
		res, runErr := runner.Run(ctx, env)
		if runErr != nil {
			emit(NodeFailed{NodeID: cur.ID, NodeType: cur.Data.NodeType(), Error: runErr.Error()})
			return nil, fmt.Errorf("node %q (%s) failed: %w",
				cur.ID, cur.Data.NodeType(), runErr)
		}
		if res == nil {
			res = &RunResult{Status: StatusSucceeded}
		}
		if res.Status == StatusFailed {
			emit(NodeFailed{NodeID: cur.ID, NodeType: cur.Data.NodeType(), Error: res.Error})
			return nil, fmt.Errorf("node %q (%s) failed: %s",
				cur.ID, cur.Data.NodeType(), res.Error)
		}

		// Commit outputs.
		if len(res.Outputs) > 0 {
			e.pool.AddOutputs(cur.ID, res.Outputs)
		}

		handle := res.EdgeSourceHandle
		if handle == "" {
			handle = DefaultSourceHandle
		}
		emit(NodeFinished{
			NodeID:   cur.ID,
			NodeType: cur.Data.NodeType(),
			Outputs:  res.Outputs,
			Handle:   handle,
		})

		// Terminal nodes (answer / end) finish the run.
		if isTerminal(cur.Data.NodeType()) {
			emit(WorkflowFinished{Outputs: res.Outputs})
			return &Result{
				Outputs:   res.Outputs,
				FinalNode: cur.ID,
				Steps:     steps,
			}, nil
		}

		// Pick the next node via the routing table.
		next, err := nextNode(routing, cur.ID, handle, g)
		if err != nil {
			return nil, err
		}
		cur = next
	}

	return &Result{Steps: steps}, nil
}

// isTerminal reports whether a node type ends the workflow when reached.
//
// Reference: graphon.enums.NodeExecutionType.RESPONSE
func isTerminal(t dsl.NodeType) bool {
	return t == dsl.NodeTypeEnd || t == dsl.NodeTypeAnswer
}

// seedInputs writes system variables and start-node user inputs into the pool.
func (e *Engine) seedInputs(in RunInput, g dsl.Graph) {
	if in.Query != "" {
		e.pool.SetSystem("query", in.Query)
	}
	if in.Files != nil {
		e.pool.SetSystem("files", in.Files)
	}
	if in.UserID != "" {
		e.pool.SetSystem("user_id", in.UserID)
	}
	if in.ConversationID != "" {
		e.pool.SetSystem("conversation_id", in.ConversationID)
	}
	for k, v := range in.UserInputs {
		// Make user inputs available both as sys.<k> (legacy) and via the
		// start node's id (the typical {{#start.<k>#}} reference).
		e.pool.SetSystem(k, v)
	}
	// Bind user inputs to the start node id too.
	for _, n := range g.Nodes {
		if _, ok := n.Data.(*dsl.StartNodeData); ok {
			for k, v := range in.UserInputs {
				e.pool.Add([]string{n.ID, k}, v)
			}
			if in.Query != "" {
				e.pool.Add([]string{n.ID, "query"}, in.Query)
			}
		}
	}
	// Seed environment variables (DSL declared, defaults applied).
	if e.dsl.Workflow != nil {
		for _, ev := range e.dsl.Workflow.EnvironmentVariables {
			e.pool.SetEnv(ev.Name, ev.Value)
		}
		for _, cv := range e.dsl.Workflow.ConversationVariables {
			e.pool.SetConversation(cv.Name, cv.Value)
		}
	}
}

// findRoot returns the first ROOT-typed node (start / datasource / trigger-*).
func findRoot(g dsl.Graph) (*dsl.Node, error) {
	for i := range g.Nodes {
		n := &g.Nodes[i]
		if n.IsNote() {
			continue
		}
		if dsl.IsRootNodeType(n.Data.NodeType()) {
			return n, nil
		}
	}
	return nil, errors.New("engine: graph has no root node")
}

// buildRoutingTable indexes edges by (source_id, source_handle).
type routingTable map[string]map[string][]string

func buildRoutingTable(g dsl.Graph) routingTable {
	rt := routingTable{}
	for _, e := range g.Edges {
		sh := e.EffectiveSourceHandle()
		bucket, ok := rt[e.Source]
		if !ok {
			bucket = map[string][]string{}
			rt[e.Source] = bucket
		}
		bucket[sh] = append(bucket[sh], e.Target)
	}
	return rt
}

// nextNode picks the successor node given the chosen source handle. When
// multiple targets exist for the same handle (parallel branches), only the
// first is followed in this minimal engine — graphon's full engine would
// run them concurrently via its worker pool.
func nextNode(rt routingTable, sourceID, handle string, g dsl.Graph) (*dsl.Node, error) {
	bucket, ok := rt[sourceID]
	if !ok {
		// Dangling node: treat as graceful end.
		return nil, nil
	}
	targets := bucket[handle]
	if len(targets) == 0 && handle != DefaultSourceHandle {
		// Some DSLs leave the implicit "false" branch unwired; fall back to
		// the default handle if the chosen one has no edges.
		targets = bucket[DefaultSourceHandle]
	}
	if len(targets) == 0 {
		return nil, nil
	}
	return findNodeByID(g, targets[0])
}

// findNodeByID returns a pointer to the node with the given ID.
func findNodeByID(g dsl.Graph, id string) (*dsl.Node, error) {
	for i := range g.Nodes {
		if g.Nodes[i].ID == id {
			return &g.Nodes[i], nil
		}
	}
	return nil, fmt.Errorf("engine: edge points to unknown node %q", id)
}
