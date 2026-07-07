// Command parse loads a Dify DSL YAML file and prints a structured summary
// (top-level metadata, node-type histogram, edges, validation issues).
package main

import (
	"dify-dsl-parser/dsl"
	"fmt"
	"os"
	"sort"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: parse <dsl.yml> [<dsl.yml> ...]")
		os.Exit(2)
	}
	failures := 0
	for _, path := range os.Args[1:] {
		fmt.Printf("=== %s ===\n", path)
		d, err := dsl.ParseFile(path)
		if err != nil {
			fmt.Printf("  ERROR: %v\n\n", err)
			failures++
			continue
		}
		printSummary(d)
		fmt.Println()
	}
	if failures > 0 {
		os.Exit(1)
	}
}

func printSummary(d *dsl.DSL) {
	fmt.Printf("  version : %s\n", d.Version)
	fmt.Printf("  kind    : %s\n", d.Kind)
	fmt.Printf("  app     : name=%q mode=%s\n", d.App.Name, d.App.Mode)

	if !d.IsWorkflow() {
		fmt.Println("  (non-workflow app: model_config based)")
		return
	}

	g := d.Workflow.Graph
	fmt.Printf("  nodes   : %d\n", len(g.Nodes))
	fmt.Printf("  edges   : %d\n", len(g.Edges))

	// Node-type histogram.
	hist := map[string]int{}
	for _, n := range g.Nodes {
		t := n.Data.NodeType()
		if t == "" {
			t = "(custom-note)"
		}
		hist[t]++
	}
	keys := make([]string, 0, len(hist))
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Println("  types   :")
	for _, k := range keys {
		fmt.Printf("            %-22s %d\n", k, hist[k])
	}

	// SourceHandle histogram (a peek at the routing topology).
	handleHist := map[string]int{}
	for _, e := range g.Edges {
		handleHist[e.EffectiveSourceHandle()]++
	}
	if len(handleHist) > 0 {
		fmt.Println("  handles :")
		hKeys := make([]string, 0, len(handleHist))
		for k := range handleHist {
			hKeys = append(hKeys, k)
		}
		sort.Strings(hKeys)
		for _, k := range hKeys {
			fmt.Printf("            %-22s %d\n", k, handleHist[k])
		}
	}

	// Validate.
	issues := d.Validate()
	if len(issues) == 0 {
		fmt.Println("  validate: OK")
	} else {
		fmt.Printf("  validate: %d issue(s)\n", len(issues))
		fmt.Println(dsl.FormatIssues(issues))
	}
}
