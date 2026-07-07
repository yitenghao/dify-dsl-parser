package engine

import (
	"context"
	"fmt"
	"strings"

	"dify-dsl-parser/dsl"
)

// ----------------------------------------------------------------------------
// start: pure pass-through. The engine has already seeded user inputs into
// the pool under the start node's id, so the runner only reports them as
// outputs (which causes the engine to write them again under the same key —
// idempotent).
// ----------------------------------------------------------------------------

func init() {
	RegisterRunner(dsl.NodeTypeStart, runnerFunc(runStart))
	RegisterRunner(dsl.NodeTypeEnd, runnerFunc(runEnd))
	RegisterRunner(dsl.NodeTypeAnswer, runnerFunc(runAnswer))
	RegisterRunner(dsl.NodeTypeTemplateTransform, runnerFunc(runTemplateTransform))
	RegisterRunner(dsl.NodeTypeVariableAggregator, runnerFunc(runVariableAggregator))
	RegisterRunner(dsl.NodeTypeAssigner, runnerFunc(runVariableAssigner))
	RegisterRunner(dsl.NodeTypeLegacyAggregator, runnerFunc(runVariableAssigner))
	// iteration-start / loop-start are subgraph anchors with no business
	// logic; they're a pass-through so traversal can continue.
	RegisterRunner(dsl.NodeTypeIterationStart, runnerFunc(runPassThrough))
	RegisterRunner(dsl.NodeTypeLoopStart, runnerFunc(runPassThrough))
	RegisterRunner(dsl.NodeTypeLoopEnd, runnerFunc(runPassThrough))
}

func runStart(_ context.Context, env *RunEnv) (*RunResult, error) {
	d, ok := env.Node.Data.(*dsl.StartNodeData)
	if !ok {
		return nil, fmt.Errorf("start: unexpected data type %T", env.Node.Data)
	}
	outputs := map[string]any{}
	// Re-publish each declared variable from the pool (seeded by the engine).
	for _, v := range d.Variables {
		if val, found := env.Pool.Get([]string{env.Node.ID, v.Variable}); found {
			outputs[v.Variable] = val
		} else if v.Default != nil {
			outputs[v.Variable] = v.Default
		}
	}
	return &RunResult{Status: StatusSucceeded, Outputs: outputs}, nil
}

func runPassThrough(_ context.Context, _ *RunEnv) (*RunResult, error) {
	return &RunResult{Status: StatusSucceeded}, nil
}

// ----------------------------------------------------------------------------
// end: collect declared outputs from the pool and return them.
// Reference: graphon.nodes.end.end_node.EndNode._run
// ----------------------------------------------------------------------------

func runEnd(_ context.Context, env *RunEnv) (*RunResult, error) {
	d, ok := env.Node.Data.(*dsl.EndNodeData)
	if !ok {
		return nil, fmt.Errorf("end: unexpected data type %T", env.Node.Data)
	}
	outs := map[string]any{}
	for _, o := range d.Outputs {
		if v, found := env.Pool.Get(o.ValueSelector); found {
			outs[o.Variable] = v
		} else {
			outs[o.Variable] = nil
		}
	}
	return &RunResult{Status: StatusSucceeded, Outputs: outs}, nil
}

// ----------------------------------------------------------------------------
// answer: render the answer template against the pool.
// Reference: graphon.nodes.answer.answer_node.AnswerNode._run
// ----------------------------------------------------------------------------

func runAnswer(_ context.Context, env *RunEnv) (*RunResult, error) {
	d, ok := env.Node.Data.(*dsl.AnswerNodeData)
	if !ok {
		return nil, fmt.Errorf("answer: unexpected data type %T", env.Node.Data)
	}
	rendered := RenderTemplate(d.Answer, env.Pool)
	// Emit the full answer as a single chunk so streaming consumers see it.
	env.Emit(StreamChunk{NodeID: env.Node.ID, Delta: rendered})
	return &RunResult{
		Status:  StatusSucceeded,
		Outputs: map[string]any{"answer": rendered},
	}, nil
}

// ----------------------------------------------------------------------------
// template-transform: render the template using the declared variable
// bindings.
//
// graphon uses real Jinja2 here. For Go parity without pulling a heavy
// templating dependency, we support the common subset: {{ var }}, {{ var.field }},
// and {{ var.0 }}. This covers the overwhelming majority of real-world
// Dify template-transform nodes (which are typically a one-line interpolation).
//
// Reference: graphon.nodes.template_transform.template_transform_node._run
// ----------------------------------------------------------------------------

func runTemplateTransform(_ context.Context, env *RunEnv) (*RunResult, error) {
	d, ok := env.Node.Data.(*dsl.TemplateTransformNodeData)
	if !ok {
		return nil, fmt.Errorf("template-transform: unexpected data type %T", env.Node.Data)
	}
	// Resolve declared variables.
	vars := map[string]any{}
	for _, vs := range d.Variables {
		if v, found := env.Pool.Get(vs.ValueSelector); found {
			vars[vs.Variable] = v
		} else {
			vars[vs.Variable] = nil
		}
	}
	out := renderJinjaLite(d.Template, vars)
	return &RunResult{
		Status: StatusSucceeded,
		Inputs: vars,
		Outputs: map[string]any{
			"output": out,
		},
	}, nil
}

// renderJinjaLite implements a tiny subset of jinja2 sufficient for the
// template-transform fixtures: {{ name }}, {{ name.field }}, {{ name.0 }}.
//
// It does not support {% %} blocks, filters, or expressions; if a fixture
// needs them the runner will simply emit the raw {{ ... }} placeholder.
func renderJinjaLite(template string, vars map[string]any) string {
	var out strings.Builder
	i := 0
	for i < len(template) {
		// Find next {{
		open := strings.Index(template[i:], "{{")
		if open < 0 {
			out.WriteString(template[i:])
			break
		}
		open += i
		out.WriteString(template[i:open])

		close := strings.Index(template[open:], "}}")
		if close < 0 {
			// Unbalanced; emit the rest verbatim.
			out.WriteString(template[open:])
			break
		}
		close += open
		expr := strings.TrimSpace(template[open+2 : close])
		out.WriteString(stringify(resolveJinjaPath(expr, vars)))
		i = close + 2
	}
	return out.String()
}

func resolveJinjaPath(expr string, vars map[string]any) any {
	parts := strings.Split(expr, ".")
	if len(parts) == 0 {
		return nil
	}
	v, ok := vars[parts[0]]
	if !ok {
		return nil
	}
	for _, seg := range parts[1:] {
		nv, ok := walk(v, seg)
		if !ok {
			return nil
		}
		v = nv
	}
	return v
}

// ----------------------------------------------------------------------------
// variable-aggregator: pick the first non-nil value from the candidate list.
// Reference: graphon.nodes.variable_aggregator.variable_aggregator_node._run
// ----------------------------------------------------------------------------

func runVariableAggregator(_ context.Context, env *RunEnv) (*RunResult, error) {
	d, ok := env.Node.Data.(*dsl.VariableAggregatorNodeData)
	if !ok {
		return nil, fmt.Errorf("variable-aggregator: unexpected data type %T", env.Node.Data)
	}
	for _, sel := range d.Variables {
		if v, found := env.Pool.Get(sel); found && v != nil {
			return &RunResult{
				Status:  StatusSucceeded,
				Outputs: map[string]any{"output": v},
			}, nil
		}
	}
	// All candidates absent.
	return &RunResult{
		Status:  StatusSucceeded,
		Outputs: map[string]any{"output": nil},
	}, nil
}

// ----------------------------------------------------------------------------
// assigner (v2) / variable-assigner (legacy): write to a variable target.
// Reference: graphon.nodes.variable_assigner.v2.variable_assigner_node._run
// ----------------------------------------------------------------------------

func runVariableAssigner(_ context.Context, env *RunEnv) (*RunResult, error) {
	d, ok := env.Node.Data.(*dsl.VariableAssignerNodeData)
	if !ok {
		return nil, fmt.Errorf("assigner: unexpected data type %T", env.Node.Data)
	}
	for _, item := range d.Items {
		if len(item.VariableSelector) < 2 {
			continue
		}
		// Determine the source value.
		var source any
		switch item.InputType {
		case "constant":
			source = item.Value
		case "variable":
			if sel, ok := item.Value.([]any); ok {
				selStr := make([]string, 0, len(sel))
				for _, s := range sel {
					selStr = append(selStr, fmt.Sprint(s))
				}
				if v, found := env.Pool.Get(selStr); found {
					source = v
				}
			}
		default:
			source = item.Value
		}

		// Apply the operation. Most common ops: over-write / append / set / clear.
		switch item.Operation {
		case "clear":
			env.Pool.Add(item.VariableSelector, nil)
		case "append":
			cur, _ := env.Pool.Get(item.VariableSelector)
			arr, _ := cur.([]any)
			env.Pool.Add(item.VariableSelector, append(arr, source))
		case "extend":
			cur, _ := env.Pool.Get(item.VariableSelector)
			arr, _ := cur.([]any)
			if more, ok := source.([]any); ok {
				arr = append(arr, more...)
			}
			env.Pool.Add(item.VariableSelector, arr)
		default: // over-write / set / =
			env.Pool.Add(item.VariableSelector, source)
		}
	}
	// Legacy v1 single-target assigner.
	if len(d.AssignedVariableSelector) >= 2 && len(d.InputVariableSelector) >= 2 {
		if v, found := env.Pool.Get(d.InputVariableSelector); found {
			env.Pool.Add(d.AssignedVariableSelector, v)
		}
	}
	return &RunResult{Status: StatusSucceeded}, nil
}
