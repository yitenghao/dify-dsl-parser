// Command export parses a Dify DSL YAML file and re-emits it. Useful for:
//   - normalising DSL files (canonical field ordering)
//   - sanity-checking that a DSL round-trips cleanly through this parser
//   - applying simple transforms before saving (--rename-model)
//
// Examples:
//
//	# Echo a DSL to stdout (verbatim where possible):
//	export input.yml
//
//	# Save into a new file, with default 2-space indent:
//	export -o normalized.yml input.yml
//
//	# Force fresh encoding from typed structs (drops cached raw payloads):
//	export -fresh -o canonical.yml input.yml
//
//	# Replace every llm.model.name occurrence and write back:
//	export -rename-model gpt-4o -o updated.yml input.yml
package main

import (
	"flag"
	"fmt"
	"os"

	"dify-dsl-parser/dsl"
)

func main() {
	var (
		outPath     = flag.String("o", "", "output path (defaults to stdout)")
		fresh       = flag.Bool("fresh", false, "drop cached raw YAML and re-encode every node from typed structs")
		indent      = flag.Int("indent", 2, "spaces per indent level")
		renameModel = flag.String("rename-model", "", "if set, rewrites every llm node's model.name to this value")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr,
			"usage: export [-o out.yml] [-fresh] [-indent N] [-rename-model NAME] <input.yml>\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	in := flag.Arg(0)

	d, err := dsl.ParseFile(in)
	if err != nil {
		fail("parse: %v", err)
	}

	// Optional transform: rename every llm node's model.
	if *renameModel != "" && d.IsWorkflow() {
		count := 0
		for i := range d.Workflow.Graph.Nodes {
			n := &d.Workflow.Graph.Nodes[i]
			if llm, ok := n.Data.(*dsl.LLMNodeData); ok {
				llm.Model.Name = *renameModel
				n.MarkDataDirty()
				count++
			}
		}
		fmt.Fprintf(os.Stderr, "renamed model on %d llm node(s)\n", count)
	}

	opts := dsl.EncodeOptions{Indent: *indent, FreshEncoding: *fresh}

	if *outPath == "" {
		if err := d.EncodeWithOptions(os.Stdout, opts); err != nil {
			fail("encode: %v", err)
		}
		return
	}
	if err := d.WriteFileWithOptions(*outPath, opts); err != nil {
		fail("write: %v", err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", *outPath)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "export: "+format+"\n", args...)
	os.Exit(1)
}
