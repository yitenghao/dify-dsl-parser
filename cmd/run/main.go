// Command run executes a Dify DSL file end-to-end, using a built-in mock
// LLM unless a real one is wired up. It prints every engine event so you
// can watch the workflow advance node by node.
//
// Examples:
//
//	# Run with the canned mock LLM, sending "say hi" as the query:
//	run -q "say hi" testdata/basic_llm_chat_workflow.yml
//
//	# Run an if-else branching workflow with a particular query:
//	run -q "hello world" testdata/conditional_hello_branching_workflow.yml
//
//	# Pass JSON-encoded user inputs:
//	run -inputs '{"switch1": 1, "switch2": 0}' testdata/dual_switch_variable_aggregator_workflow.yml
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"dify-dsl-parser/dsl"
	"dify-dsl-parser/engine"
)

func main() {
	var (
		query    = flag.String("q", "", "shorthand for sys.query and start_node.query")
		userIn   = flag.String("inputs", "", "JSON object of user inputs (start node form fields)")
		llmReply = flag.String("llm-reply", "[mock] hello from llm", "the canned reply the mock LLM returns")
	)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr,
			"usage: run [-q QUERY] [-inputs JSON] [-llm-reply TEXT] <dsl.yml>")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	d, err := dsl.ParseFile(flag.Arg(0))
	if err != nil {
		fail("parse: %v", err)
	}

	// Decode -inputs JSON into a map.
	inputs := map[string]any{}
	if *userIn != "" {
		if err := json.Unmarshal([]byte(*userIn), &inputs); err != nil {
			fail("decode -inputs: %v", err)
		}
	}

	llm := &engine.MockLLM{Reply: *llmReply}
	tool := &engine.MockTool{}

	hooks := engine.Hooks{
		OnEvent: func(ev engine.Event) {
			switch e := ev.(type) {
			case engine.NodeStarted:
				fmt.Printf("  ▶ %-22s %s\n", e.NodeType, e.NodeID)
			case engine.NodeFinished:
				outs := compactJSON(e.Outputs)
				if len(outs) > 80 {
					outs = outs[:77] + "..."
				}
				fmt.Printf("  ✓ %-22s %s  → handle=%s outputs=%s\n",
					e.NodeType, e.NodeID, e.Handle, outs)
			case engine.NodeFailed:
				fmt.Printf("  ✗ %-22s %s  ERROR=%s\n", e.NodeType, e.NodeID, e.Error)
			case engine.StreamChunk:
				// Don't spam: prefix with a marker, single line.
				fmt.Printf("    ⚡ %s\n", strings.ReplaceAll(e.Delta, "\n", " "))
			case engine.WorkflowFinished:
				fmt.Println("=== WORKFLOW FINISHED ===")
			}
		},
	}

	eng := engine.New(d).WithLLM(llm).WithTool(tool).WithHooks(hooks)

	fmt.Printf("=== running %s (mode=%s) ===\n", d.App.Name, d.App.Mode)
	res, err := eng.Run(context.Background(), engine.RunInput{
		Query:      *query,
		UserInputs: inputs,
	})
	if err != nil {
		fail("run: %v", err)
	}
	fmt.Printf("\nresult: final_node=%s steps=%d\n", res.FinalNode, res.Steps)
	fmt.Println("outputs:")
	for k, v := range res.Outputs {
		fmt.Printf("  %s = %s\n", k, compactJSON(v))
	}
}

func compactJSON(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "run: "+format+"\n", args...)
	os.Exit(1)
}
