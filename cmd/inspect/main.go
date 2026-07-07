// Command inspect dumps a deep view of a single DSL: every node's title,
// type, version, and the variable references discovered statically inside it
// (both structured selectors and template strings).
package main

import (
	"dify-dsl-parser/dsl"
	"fmt"
	"os"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: inspect <dsl.yml>")
		os.Exit(2)
	}
	d, err := dsl.ParseFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse: %v\n", err)
		os.Exit(1)
	}
	if !d.IsWorkflow() {
		fmt.Println("not a workflow app")
		return
	}

	fmt.Printf("App      : %s (mode=%s, dsl=%s)\n", d.App.Name, d.App.Mode, d.Version)
	fmt.Printf("Nodes    : %d, Edges: %d\n\n",
		len(d.Workflow.Graph.Nodes), len(d.Workflow.Graph.Edges))

	for i := range d.Workflow.Graph.Nodes {
		n := &d.Workflow.Graph.Nodes[i]
		base := n.Data.Base()
		fmt.Printf("[node %s]\n", n.ID)
		fmt.Printf("  type    : %s (v%s)\n", n.Data.NodeType(), defaultStr(base.Version, "1"))
		fmt.Printf("  title   : %q\n", base.Title)
		if base.Desc != "" {
			fmt.Printf("  desc    : %q\n", base.Desc)
		}

		// Type-specific summary highlights.
		switch td := n.Data.(type) {
		case *dsl.LLMNodeData:
			fmt.Printf("  model   : %s/%s mode=%s\n", td.Model.Provider, td.Model.Name, td.Model.Mode)
			if msgs, ok := td.PromptMessages(); ok {
				for _, m := range msgs {
					prev := m.Text
					if len(prev) > 60 {
						prev = prev[:60] + "..."
					}
					fmt.Printf("  prompt  : [%s] %q\n", m.Role, prev)
				}
			}
		case *dsl.IfElseNodeData:
			cases := td.IterCases()
			fmt.Printf("  cases   : %d\n", len(cases))
			for _, c := range cases {
				fmt.Printf("            case_id=%s op=%s conditions=%d\n",
					c.CaseID, c.LogicalOperator, len(c.Conditions))
			}
		case *dsl.HTTPRequestNodeData:
			fmt.Printf("  http    : %s %s\n", td.Method, td.URL)
			if hdrs := dsl.ParseHTTPLineMap(td.Headers); len(hdrs) > 0 {
				for k, v := range hdrs {
					fmt.Printf("    header: %s = %s\n", k, v)
				}
			}
		case *dsl.ToolNodeData:
			fmt.Printf("  tool    : %s/%s (provider=%s)\n",
				td.ProviderID, td.ToolName, td.ProviderType)
			for k, p := range td.ToolParameters {
				fmt.Printf("    param : %s [%s] = %v\n", k, p.Type, p.Value)
			}
		case *dsl.IterationNodeData:
			fmt.Printf("  iter    : iter=%v out=%v parallel=%v\n",
				td.IteratorSelector, td.OutputSelector, td.IsParallel)
		case *dsl.LoopNodeData:
			fmt.Printf("  loop    : count=%d break=%d\n",
				td.LoopCount, len(td.BreakConditions))
		case *dsl.AnswerNodeData:
			a := td.Answer
			if len(a) > 80 {
				a = a[:80] + "..."
			}
			fmt.Printf("  answer  : %q\n", a)
		case *dsl.EndNodeData:
			for _, o := range td.Outputs {
				fmt.Printf("  output  : %s = %v (type=%s)\n",
					o.Variable, o.ValueSelector, o.ValueType)
			}
		case *dsl.VariableAssignerNodeData:
			for _, it := range td.Items {
				fmt.Printf("  assign  : %v %s %v (input=%s)\n",
					it.VariableSelector, it.Operation, it.Value, it.InputType)
			}
		}

		// Variable references statically discovered.
		refs := dsl.CollectNodeReferences(n)
		if len(refs) > 0 {
			fmt.Printf("  refs    : %d variable reference(s)\n", len(refs))
			for _, r := range refs {
				note := strings.Join(r.Selector, ".")
				if r.Raw != "" && !strings.HasPrefix(r.Raw, n.ID) {
					note = fmt.Sprintf("%-40s  raw=%s", note, r.Raw)
				}
				fmt.Printf("    ref   : %s\n", note)
			}
		}
		fmt.Println()
	}
}

func defaultStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
