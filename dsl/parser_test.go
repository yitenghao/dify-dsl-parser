package dsl_test

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"dify-dsl-parser/dsl"
)

const fixturesDir = "../testdata"

// listFixtures returns every *.yml file in the fixtures directory.
func listFixtures(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(fixturesDir)
	if err != nil {
		t.Fatalf("read fixtures dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml") {
			continue
		}
		out = append(out, filepath.Join(fixturesDir, name))
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatalf("no fixtures under %s", fixturesDir)
	}
	return out
}

// TestParseAllFixtures parses every Dify DSL YAML in testdata/ and asserts
// the most basic invariants.
func TestParseAllFixtures(t *testing.T) {
	for _, path := range listFixtures(t) {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			d, err := dsl.ParseFile(path)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			if d.Kind != "app" {
				t.Errorf("kind = %q, want %q", d.Kind, "app")
			}
			if d.App.Mode == "" {
				t.Error("app.mode is empty")
			}

			if !d.IsWorkflow() {
				t.Skip("not a workflow app")
			}
			if d.Workflow == nil {
				t.Fatal("workflow block missing")
			}
			if len(d.Workflow.Graph.Nodes) == 0 {
				t.Error("graph.nodes is empty")
			}

			// Every node must have a non-empty ID.
			for i, n := range d.Workflow.Graph.Nodes {
				if n.ID == "" {
					t.Errorf("node[%d] has empty ID", i)
				}
				if n.Data == nil {
					t.Errorf("node[%d] (%s) has nil Data", i, n.ID)
				}
			}

			// Every edge must point at known nodes (we expect 0 issues here
			// for the curated fixtures).
			ids := map[string]struct{}{}
			for _, n := range d.Workflow.Graph.Nodes {
				if n.IsNote() {
					continue
				}
				ids[n.ID] = struct{}{}
			}
			for _, e := range d.Workflow.Graph.Edges {
				if _, ok := ids[e.Source]; !ok {
					t.Errorf("edge %s source %q not found", e.ID, e.Source)
				}
				if _, ok := ids[e.Target]; !ok {
					t.Errorf("edge %s target %q not found", e.ID, e.Target)
				}
			}
		})
	}
}

// TestNodeTypeDispatch asserts that for a known fixture, the polymorphic
// decoder returns the right concrete struct for each node type.
func TestNodeTypeDispatch(t *testing.T) {
	d, err := dsl.ParseFile(filepath.Join(fixturesDir, "basic_llm_chat_workflow.yml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	wantTypes := map[string]bool{
		dsl.NodeTypeStart: false,
		dsl.NodeTypeLLM:   false,
		dsl.NodeTypeEnd:   false,
	}
	for _, n := range d.Workflow.Graph.Nodes {
		switch n.Data.(type) {
		case *dsl.StartNodeData:
			wantTypes[dsl.NodeTypeStart] = true
		case *dsl.LLMNodeData:
			wantTypes[dsl.NodeTypeLLM] = true
		case *dsl.EndNodeData:
			wantTypes[dsl.NodeTypeEnd] = true
		}
	}
	for ty, seen := range wantTypes {
		if !seen {
			t.Errorf("expected to see node type %q decoded, did not", ty)
		}
	}
}

// TestNoUnknownNodeTypes makes sure none of the curated fixtures fall through
// to UnknownNodeData (which would mean we forgot to register a type).
func TestNoUnknownNodeTypes(t *testing.T) {
	for _, path := range listFixtures(t) {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			d, err := dsl.ParseFile(path)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !d.IsWorkflow() {
				t.Skip("non-workflow app")
			}
			for _, n := range d.Workflow.Graph.Nodes {
				if n.IsNote() {
					continue
				}
				if _, isUnknown := n.Data.(*dsl.UnknownNodeData); isUnknown {
					t.Errorf("node %s (data.type=%s) decoded as UnknownNodeData",
						n.ID, n.Data.NodeType())
				}
			}
		})
	}
}

// TestExtractTemplateRefs spot-checks the template variable regex against the
// shape used by Dify.
func TestExtractTemplateRefs(t *testing.T) {
	cases := []struct {
		in   string
		want [][]string // expected selectors
	}{
		{"hello {{#start.query#}}", [][]string{{"start", "query"}}},
		{"a={{#sys.user_id#}} b={{#llm.text#}}", [][]string{{"sys", "user_id"}, {"llm", "text"}}},
		{"deep {{#node.body.items.0#}}", nil}, // first segment after node must be a letter
		{"none here", nil},
		{"", nil},
		{"{{#node.a.b.c.d#}}", [][]string{{"node", "a", "b", "c", "d"}}},
	}
	for _, c := range cases {
		got := dsl.ExtractTemplateRefs(c.in)
		if len(got) != len(c.want) {
			t.Errorf("input %q: got %d refs, want %d (%v)",
				c.in, len(got), len(c.want), got)
			continue
		}
		for i, g := range got {
			if !equalStringSlices(g.Selector, c.want[i]) {
				t.Errorf("input %q ref[%d]: got %v, want %v",
					c.in, i, g.Selector, c.want[i])
			}
		}
	}
}

// TestParseHTTPLineMap verifies the headers/params line format parser.
func TestParseHTTPLineMap(t *testing.T) {
	in := "Content-Type:application/json\nX-Trace-Id:{{#sys.user_id#}}\n  \n"
	got := dsl.ParseHTTPLineMap(in)
	if got["Content-Type"] != "application/json" {
		t.Errorf("Content-Type: got %q", got["Content-Type"])
	}
	if got["X-Trace-Id"] != "{{#sys.user_id#}}" {
		t.Errorf("X-Trace-Id: got %q", got["X-Trace-Id"])
	}
	if len(got) != 2 {
		t.Errorf("got %d entries, want 2: %v", len(got), got)
	}
}

// TestRawDataPreserved ensures every node retains the original yaml.Node so
// callers can re-decode against custom schemas.
func TestRawDataPreserved(t *testing.T) {
	d, err := dsl.ParseFile(filepath.Join(fixturesDir, "basic_llm_chat_workflow.yml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, n := range d.Workflow.Graph.Nodes {
		if n.IsNote() {
			continue
		}
		if n.RawData() == nil {
			t.Errorf("node %s has nil RawData", n.ID)
		}
	}
}

// TestRoundTrip ensures we can re-marshal a parsed DSL back to YAML without
// losing the node payload (we compare the fundamental fields).
func TestRoundTrip(t *testing.T) {
	d, err := dsl.ParseFile(filepath.Join(fixturesDir, "basic_llm_chat_workflow.yml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	buf := &bytes.Buffer{}
	enc := yaml.NewEncoder(buf)
	enc.SetIndent(2)
	if err := enc.Encode(d); err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	enc.Close()

	d2, err := dsl.Parse(buf)
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if d2.Version != d.Version {
		t.Errorf("version drift: %q -> %q", d.Version, d2.Version)
	}
	if len(d2.Workflow.Graph.Nodes) != len(d.Workflow.Graph.Nodes) {
		t.Errorf("node count drift: %d -> %d",
			len(d.Workflow.Graph.Nodes), len(d2.Workflow.Graph.Nodes))
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
