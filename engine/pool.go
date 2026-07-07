// Package engine implements a workflow execution engine for parsed Dify DSL
// graphs.
//
// The design is a pragmatic Go transcription of graphon's runtime model
// (variable pool + per-node Run() + edge-handle routing), without graphon's
// production-grade worker pool / command channel layer. It is single-goroutine
// per workflow run and small enough to embed.
package engine

import (
	"fmt"
	"strconv"
	"sync"
)

// VariablePool stores all variable values produced and consumed during a
// workflow execution.
//
// The shape mirrors graphon.runtime.variable_pool.VariablePool:
//
//	pool[node_id][var_name] = value
//
// The first selector segment is the node id (or one of the reserved
// namespaces sys / env / conversation / rag).
//
// Lookups support nested paths: a selector longer than two segments walks
// into map / slice values (for example ["http_node", "body", "items", "0"]).
//
// VariablePool is safe for concurrent reads but not concurrent writes; in
// single-goroutine workflow runs (the default) no locking is needed.
type VariablePool struct {
	mu   sync.RWMutex
	data map[string]map[string]any
}

// NewVariablePool returns an empty pool.
func NewVariablePool() *VariablePool {
	return &VariablePool{data: map[string]map[string]any{}}
}

// SetSystem seeds the pool with system variables (selector prefix "sys").
// Typical keys are "query", "files", "user_id", "conversation_id".
func (p *VariablePool) SetSystem(name string, value any) {
	p.Add([]string{"sys", name}, value)
}

// SetEnv seeds the pool with an environment variable (selector prefix "env").
func (p *VariablePool) SetEnv(name string, value any) {
	p.Add([]string{"env", name}, value)
}

// SetConversation seeds a conversation-scoped variable.
func (p *VariablePool) SetConversation(name string, value any) {
	p.Add([]string{"conversation", name}, value)
}

// Add stores a value under the given selector. The selector must have at
// least two segments (node_id, var_name); extra segments are silently
// ignored on writes.
//
// Reference: graphon.runtime.variable_pool.VariablePool.add
func (p *VariablePool) Add(selector []string, value any) {
	if len(selector) < 2 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	bucket, ok := p.data[selector[0]]
	if !ok {
		bucket = map[string]any{}
		p.data[selector[0]] = bucket
	}
	bucket[selector[1]] = value
}

// AddOutputs writes every entry in outputs under the given node_id.
// This is the standard way an engine commits a NodeRunResult.
func (p *VariablePool) AddOutputs(nodeID string, outputs map[string]any) {
	if len(outputs) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	bucket, ok := p.data[nodeID]
	if !ok {
		bucket = make(map[string]any, len(outputs))
		p.data[nodeID] = bucket
	}
	for k, v := range outputs {
		bucket[k] = v
	}
}

// Get retrieves a value by selector, walking nested maps / slices for
// selectors longer than two segments. Returns (nil, false) when any segment
// can't be resolved.
//
// Reference: graphon.runtime.variable_pool.VariablePool.get
func (p *VariablePool) Get(selector []string) (any, bool) {
	if len(selector) < 2 {
		return nil, false
	}
	p.mu.RLock()
	bucket, ok := p.data[selector[0]]
	if !ok {
		p.mu.RUnlock()
		return nil, false
	}
	cur, ok := bucket[selector[1]]
	p.mu.RUnlock()
	if !ok {
		return nil, false
	}
	for _, seg := range selector[2:] {
		next, ok := walk(cur, seg)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// MustGet is a convenience that panics when the selector can't be resolved.
func (p *VariablePool) MustGet(selector []string) any {
	v, ok := p.Get(selector)
	if !ok {
		panic(fmt.Sprintf("variable pool: selector %v not found", selector))
	}
	return v
}

// Snapshot returns a shallow copy of the entire pool, useful for inspection
// and event emission.
func (p *VariablePool) Snapshot() map[string]map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]map[string]any, len(p.data))
	for ns, bucket := range p.data {
		copyBucket := make(map[string]any, len(bucket))
		for k, v := range bucket {
			copyBucket[k] = v
		}
		out[ns] = copyBucket
	}
	return out
}

// walk descends one level into a map or slice value using the given segment
// as either a string key or a numeric index.
func walk(v any, segment string) (any, bool) {
	switch x := v.(type) {
	case map[string]any:
		out, ok := x[segment]
		return out, ok
	case map[any]any: // yaml-decoded maps
		for k, vv := range x {
			if fmt.Sprint(k) == segment {
				return vv, true
			}
		}
		return nil, false
	case []any:
		i, err := strconv.Atoi(segment)
		if err != nil || i < 0 || i >= len(x) {
			return nil, false
		}
		return x[i], true
	case []string:
		i, err := strconv.Atoi(segment)
		if err != nil || i < 0 || i >= len(x) {
			return nil, false
		}
		return x[i], true
	}
	return nil, false
}
