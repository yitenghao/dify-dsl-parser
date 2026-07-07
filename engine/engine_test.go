package engine_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"dify-dsl-parser/dsl"
	"dify-dsl-parser/engine"
)

const fixturesDir = "../testdata"

// TestRunBasicLLMWorkflow runs the canonical start → llm → end DSL with
// a mock LLM and asserts the end-node output contains the LLM reply.
func TestRunBasicLLMWorkflow(t *testing.T) {
	d, err := dsl.ParseFile(filepath.Join(fixturesDir, "basic_llm_chat_workflow.yml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Install a mock LLM that echoes the user query.
	llm := &engine.MockLLM{Reply: "Hello, world!"}

	var streamed strings.Builder
	hooks := engine.Hooks{
		OnEvent: func(ev engine.Event) {
			if c, ok := ev.(engine.StreamChunk); ok {
				streamed.WriteString(c.Delta)
			}
		},
	}

	eng := engine.New(d).WithLLM(llm).WithHooks(hooks)
	res, err := eng.Run(context.Background(), engine.RunInput{
		UserInputs: map[string]any{"query": "say hi"},
		Query:      "say hi",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.FinalNode == "" {
		t.Error("FinalNode is empty")
	}
	if res.Steps == 0 {
		t.Error("Steps is zero")
	}

	// The fixture's end node has a single output called "answer" mapped to
	// the llm node's text.
	got, ok := res.Outputs["answer"]
	if !ok {
		t.Fatalf("expected 'answer' in outputs, got: %v", res.Outputs)
	}
	if got != "Hello, world!" {
		t.Errorf("end.answer = %q, want %q", got, "Hello, world!")
	}
	if !strings.Contains(streamed.String(), "Hello, world!") {
		t.Errorf("stream did not contain LLM reply: %q", streamed.String())
	}
}

// TestRunIfElseBranching loads a conditional fixture and asserts the
// engine selects the right branch based on the input.
func TestRunIfElseBranching(t *testing.T) {
	d, err := dsl.ParseFile(filepath.Join(fixturesDir, "conditional_hello_branching_workflow.yml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Inspect the fixture so the test self-explains: two end nodes, gated
	// by an if-else on sys.query.
	wantNodes := map[dsl.NodeType]int{
		dsl.NodeTypeStart:  1,
		dsl.NodeTypeIfElse: 1,
		dsl.NodeTypeEnd:    2,
	}
	got := map[dsl.NodeType]int{}
	for _, n := range d.Workflow.Graph.Nodes {
		got[n.Data.NodeType()]++
	}
	for k, v := range wantNodes {
		if got[k] != v {
			t.Fatalf("fixture has %d %s nodes, expected %d (full: %v)",
				got[k], k, v, got)
		}
	}

	// Drive the engine twice: once where the condition matches, once where it doesn't.
	for _, tc := range []struct {
		name  string
		query string
	}{
		{"matches_hello", "hello world"},
		{"matches_other", "goodbye"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng := engine.New(d)
			res, err := eng.Run(context.Background(), engine.RunInput{
				Query: tc.query,
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if res.FinalNode == "" {
				t.Error("FinalNode is empty")
			}
			t.Logf("query=%q -> final node %s, outputs %v",
				tc.query, res.FinalNode, res.Outputs)
		})
	}
}

// TestRunVariableAggregator covers the aggregator fixture: two parallel
// branches feed into a single aggregator node.
func TestRunVariableAggregator(t *testing.T) {
	d, err := dsl.ParseFile(filepath.Join(fixturesDir, "dual_switch_variable_aggregator_workflow.yml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	llm := &engine.MockLLM{Reply: "AGGREGATED"}
	eng := engine.New(d).WithLLM(llm)
	// The fixture's start node declares two number inputs: switch1, switch2.
	// Both if-else nodes test the corresponding switch == 1.
	res, err := eng.Run(context.Background(), engine.RunInput{
		UserInputs: map[string]any{
			"switch1": 1,
			"switch2": 0,
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	t.Logf("final node=%s outputs=%v", res.FinalNode, res.Outputs)
}

// TestEngineWithoutLLMFailsGracefully ensures we get a clear error message
// when an llm node is reached without a configured LLM client.
func TestEngineWithoutLLMFailsGracefully(t *testing.T) {
	d, err := dsl.ParseFile(filepath.Join(fixturesDir, "basic_llm_chat_workflow.yml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	eng := engine.New(d) // no LLM
	_, err = eng.Run(context.Background(), engine.RunInput{Query: "hi"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "LLMClient") {
		t.Errorf("error message %q does not mention LLMClient", err)
	}
}

// TestVariablePoolPaths exercises the nested-path lookup that's central to
// {{#node.field.subfield#}} resolution.
func TestVariablePoolPaths(t *testing.T) {
	p := engine.NewVariablePool()
	p.Add([]string{"http", "body"}, map[string]any{
		"items": []any{
			map[string]any{"id": 1, "name": "alice"},
			map[string]any{"id": 2, "name": "bob"},
		},
	})

	if v, ok := p.Get([]string{"http", "body", "items", "0", "name"}); !ok || v != "alice" {
		t.Errorf("nested get got %v / %v, want \"alice\"", v, ok)
	}
	if _, ok := p.Get([]string{"http", "body", "items", "9", "name"}); ok {
		t.Error("expected miss for out-of-range index")
	}
}

// TestRenderTemplate spot-checks the template-string substitution.
func TestRenderTemplate(t *testing.T) {
	p := engine.NewVariablePool()
	p.SetSystem("query", "spam")
	p.Add([]string{"node1", "text"}, "ham")

	cases := []struct {
		in   string
		want string
	}{
		{"raw", "raw"},
		{"a={{#sys.query#}}", "a=spam"},
		{"a={{#sys.query#}} b={{#node1.text#}}", "a=spam b=ham"},
		{"missing={{#nope.field#}}", "missing={{#nope.field#}}"}, // unresolved kept verbatim
	}
	for _, c := range cases {
		got := engine.RenderTemplate(c.in, p)
		if got != c.want {
			t.Errorf("input %q -> %q, want %q", c.in, got, c.want)
		}
	}
}

// TestEvaluateConditionsBasics covers the comparison-operator engine.
func TestEvaluateConditionsBasics(t *testing.T) {
	p := engine.NewVariablePool()
	p.Add([]string{"node", "score"}, 75)
	p.Add([]string{"node", "name"}, "alice")

	cases := []struct {
		name  string
		conds []dsl.Condition
		op    string
		want  bool
	}{
		{
			name: "score > 60 (and)",
			conds: []dsl.Condition{
				{VariableSelector: []string{"node", "score"}, ComparisonOperator: ">", Value: 60},
			},
			op: "and", want: true,
		},
		{
			name: "score < 60 (and)",
			conds: []dsl.Condition{
				{VariableSelector: []string{"node", "score"}, ComparisonOperator: "<", Value: 60},
			},
			op: "and", want: false,
		},
		{
			name: "name contains 'lic' (and)",
			conds: []dsl.Condition{
				{VariableSelector: []string{"node", "name"}, ComparisonOperator: "contains", Value: "lic"},
			},
			op: "and", want: true,
		},
		{
			name: "or short-circuit: false || true",
			conds: []dsl.Condition{
				{VariableSelector: []string{"node", "score"}, ComparisonOperator: "<", Value: 60},
				{VariableSelector: []string{"node", "name"}, ComparisonOperator: "is", Value: "alice"},
			},
			op: "or", want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _, err := engine.EvaluateConditions(p, c.conds, c.op)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
