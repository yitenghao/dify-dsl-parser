package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"dify-dsl-parser/dsl"
	"dify-dsl-parser/engine"
)

// traceEvent is the JSON shape streamed to the editor for each engine event.
// The frontend uses `type` + `nodeId` to light up nodes on the canvas as the
// run progresses (this is the "execution tracing" the old editor lacked), and
// `delta` to render streamed LLM / answer output live.
type traceEvent struct {
	Type     string         `json:"type"` // node_started|node_finished|node_failed|chunk|finished|error
	NodeID   string         `json:"nodeId,omitempty"`
	NodeType string         `json:"nodeType,omitempty"`
	Handle   string         `json:"handle,omitempty"` // chosen source-handle (branch taken)
	Delta    string         `json:"delta,omitempty"`
	Outputs  map[string]any `json:"outputs,omitempty"`
	Error    string         `json:"error,omitempty"`
	Steps    int            `json:"steps,omitempty"`
	FinalNode string        `json:"finalNode,omitempty"`
}

func toTrace(e engine.Event) traceEvent {
	switch ev := e.(type) {
	case engine.NodeStarted:
		return traceEvent{Type: "node_started", NodeID: ev.NodeID, NodeType: ev.NodeType}
	case engine.NodeFinished:
		return traceEvent{Type: "node_finished", NodeID: ev.NodeID, NodeType: ev.NodeType, Handle: ev.Handle, Outputs: ev.Outputs}
	case engine.NodeFailed:
		return traceEvent{Type: "node_failed", NodeID: ev.NodeID, NodeType: ev.NodeType, Error: ev.Error}
	case engine.StreamChunk:
		return traceEvent{Type: "chunk", NodeID: ev.NodeID, Delta: ev.Delta}
	case engine.WorkflowFinished:
		return traceEvent{Type: "finished", Outputs: ev.Outputs}
	default:
		return traceEvent{Type: "event"}
	}
}

// runRequest is the body of POST /api/flows/{id}/run.
type runRequest struct {
	Query  string         `json:"query"`
	Inputs map[string]any `json:"inputs"`
	UserID string         `json:"userId"`
}

// handleRun executes the current draft and streams trace events as SSE.
//
// The engine's Run is synchronous and single-goroutine, calling Hooks.OnEvent
// inline; we run it in a goroutine that pushes each event onto a channel, and
// the HTTP handler drains the channel to the client, flushing per event so the
// canvas updates in real time.
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	yml, err := s.store.Draft(id)
	if err != nil {
		httpError(w, err)
		return
	}
	d, err := dsl.Parse(bytes.NewReader(yml))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "parse draft: " + err.Error()})
		return
	}

	var req runRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	events := make(chan engine.Event, 128)
	var runErr error
	var result *engine.Result

	eng := engine.New(d).WithHooks(engine.Hooks{OnEvent: func(e engine.Event) {
		// non-blocking-ish: buffered channel; if the client is slow we still
		// block the run, which is acceptable for an interactive test.
		events <- e
	}})
	if s.llm != nil {
		eng = eng.WithLLM(s.llm)
	}
	if s.tool != nil {
		eng = eng.WithTool(s.tool)
	}

	go func() {
		defer close(events)
		result, runErr = eng.Run(r.Context(), engine.RunInput{
			Query:      req.Query,
			UserInputs: req.Inputs,
			UserID:     req.UserID,
		})
	}()

	for e := range events {
		writeSSE(w, flusher, toTrace(e))
	}
	// events closed => goroutine returned => result/runErr are set.
	if runErr != nil {
		writeSSE(w, flusher, traceEvent{Type: "error", Error: runErr.Error()})
		return
	}
	final := traceEvent{Type: "done"}
	if result != nil {
		final.Steps = result.Steps
		final.FinalNode = result.FinalNode
		final.Outputs = result.Outputs
	}
	writeSSE(w, flusher, final)
}

func writeSSE(w http.ResponseWriter, f http.Flusher, ev traceEvent) {
	b, _ := json.Marshal(ev)
	fmt.Fprintf(w, "data: %s\n\n", b)
	f.Flush()
}

var _ = context.Background // keep context import if unused after edits
