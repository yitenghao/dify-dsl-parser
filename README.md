# Dify DSL Parser & Engine (Go)

A pure-Go toolkit for [Dify](https://github.com/langgenius/dify) workflow DSL files:

1. **Parser** (package `dsl`) — strongly-typed two-phase polymorphic decoder for 15+ node types, tested against 23 real Dify fixtures.
2. **Exporter** (`dsl.Marshal/Encode/WriteFile`) — round-trips parsed DSL back to YAML, with verbatim and fresh-encoding modes.
3. **Engine** (package `engine`) — single-goroutine executor that traverses a parsed graph, evaluates conditions, calls plug-in LLM/Tool clients, and streams events. Tracks the design of [graphon](https://github.com/langgenius/graphon)'s runtime model (variable pool + per-node `Run()` + edge-handle routing) without graphon's worker-pool layer.

## Why two-phase decoding?

A Dify DSL node looks like this:

```yaml
- id: my_node
  type: custom               # ← React Flow render type, NOT business type
  data:
    type: llm                # ← business type lives here
    model: { ... }           # ← schema depends on data.type
    prompt_template: [...]
```

Each `data.type` value implies a completely different schema. Go has no native
discriminated unions, so we:

1. **Phase 1** — decode every node's React-Flow envelope plus the `data`
   subtree into a generic `yaml.Node`.
2. **Phase 2** — peek at `data.type`, look up the registered factory for that
   type, and decode the raw `yaml.Node` into the concrete `*XxxNodeData`
   struct.

Unknown types fall through to `*UnknownNodeData` so a single new/extension type
never breaks a whole DSL parse.

## Directory layout

```
dify-dsl-parser/
├── go.mod
├── cmd/
│   ├── parse/              # CLI: structured summary of any DSL file
│   ├── inspect/            # CLI: deep dive on a single DSL (var refs, etc.)
│   └── export/             # CLI: re-emit a parsed DSL as YAML, with optional transforms
├── dsl/
│   ├── dsl.go              # top-level: DSL / AppMeta / Dependency / Parse()
│   ├── workflow.go         # Workflow / Graph / Edge / ScopedVariable
│   ├── node.go             # Node + UnmarshalYAML/MarshalYAML two-phase decoder
│   ├── base.go             # BaseNodeData, NodeData iface, type constants, registry
│   ├── export.go           # DSL.Marshal / WriteTo / WriteFile + EncodeOptions
│   ├── nodes_io.go         # start, end, answer
│   ├── nodes_llm.go        # llm, question-classifier, parameter-extractor, knowledge-retrieval
│   ├── nodes_logic.go      # if-else (cases + legacy conditions)
│   ├── nodes_compute.go    # code, template-transform, document-extractor
│   ├── nodes_http.go       # http-request, tool (with legacy-form ToolInput / HTTPRequestBody)
│   ├── nodes_data.go       # variable-aggregator, assigner (v1+v2), list-operator
│   ├── nodes_loop.go       # iteration, iteration-start, loop, loop-start, loop-end
│   ├── nodes_other.go      # agent, human-input, trigger-*, datasource
│   ├── selectors.go        # ExtractTemplateRefs, ParseHTTPLineMap, namespaces
│   ├── validate.go         # graph-level validation: edges, roots, subgraphs, var refs
│   └── parser_test.go      # 11 tests, 23 fixtures, full round-trip + export coverage
└── testdata/               # 23 real DSL files (DSL version 0.3.1)
```

## Quick start

```bash
# Build
go build ./...

# Summary of a DSL file
go run ./cmd/parse testdata/basic_llm_chat_workflow.yml

# Deep inspection: list every node's structure and variable references
go run ./cmd/inspect testdata/http_request_with_json_tool_workflow.yml

# Run all tests
go test ./...
```

## Programmatic use

```go
package main

import (
    "fmt"
    "dify-dsl-parser/dsl"
)

func main() {
    d, err := dsl.ParseFile("workflow.yml")
    if err != nil {
        panic(err)
    }
    fmt.Printf("%s [v%s, mode=%s]\n", d.App.Name, d.Version, d.App.Mode)

    if d.IsWorkflow() {
        for _, n := range d.Workflow.Graph.Nodes {
            // Switch on the typed Data
            switch nd := n.Data.(type) {
            case *dsl.LLMNodeData:
                fmt.Printf("LLM %s: %s/%s\n", n.ID, nd.Model.Provider, nd.Model.Name)
                if msgs, ok := nd.PromptMessages(); ok {
                    fmt.Printf("  %d chat message(s)\n", len(msgs))
                }
            case *dsl.IfElseNodeData:
                for _, c := range nd.IterCases() {
                    fmt.Printf("if-else %s: case %s, %d conditions\n",
                        n.ID, c.CaseID, len(c.Conditions))
                }
            case *dsl.HTTPRequestNodeData:
                hdrs := dsl.ParseHTTPLineMap(nd.Headers)
                fmt.Printf("HTTP %s: %s %s, %d headers\n",
                    n.ID, nd.Method, nd.URL, len(hdrs))
            }

            // Static analysis: every variable reference inside the node.
            for _, ref := range dsl.CollectNodeReferences(&n) {
                fmt.Printf("  %s -> %v (raw=%s)\n", n.ID, ref.Selector, ref.Raw)
            }
        }
    }

    // Graph-level validation
    for _, issue := range d.Validate() {
        fmt.Println("ISSUE:", issue)
    }
}
```

## Exporting back to YAML

A parsed DSL can be re-emitted as YAML, with two encoding strategies:

| Mode | When to use | What it does |
|------|-------------|--------------|
| **Verbatim** (default) | Round-tripping unmodified DSL, normalising file structure | Each node's `data` block is re-emitted verbatim from the cached raw `yaml.Node` — preserving field order and any unknown fields. Top-level keys follow the canonical Dify order (`version`, `kind`, `app`, `workflow`, `dependencies`). |
| **Fresh** | After mutating typed fields, or to produce a canonical normalised file | Cached raw payloads are dropped. Every node is re-encoded from its typed `*XxxNodeData` struct, so field order follows the Go struct definition and `omitempty` zero-values are omitted. |

### API

```go
// Default: verbatim, indent=2
b, err := d.Marshal()                // []byte
err   := d.Encode(os.Stdout)         // io.Writer
err   := d.WriteFile("out.yml")      // file

// Custom options
opts := dsl.EncodeOptions{Indent: 4, FreshEncoding: true}
b, err := d.MarshalWithOptions(opts)
err   := d.EncodeWithOptions(os.Stdout, opts)
err   := d.WriteFileWithOptions("out.yml", opts)
```

### Modifying typed fields before exporting

When you mutate fields **through** the existing typed pointer, mark the node
dirty so the next export re-encodes from the typed struct:

```go
for i := range d.Workflow.Graph.Nodes {
    n := &d.Workflow.Graph.Nodes[i]
    if llm, ok := n.Data.(*dsl.LLMNodeData); ok {
        llm.Model.Name = "claude-opus-4"
        n.MarkDataDirty()           // ← important
    }
}
d.WriteFile("updated.yml")
```

When you replace the entire `Data` value with a freshly-constructed struct,
use `SetData` (which clears the cache automatically):

```go
n.SetData(&dsl.AnswerNodeData{
    BaseNodeData: dsl.BaseNodeData{Type: dsl.NodeTypeAnswer, Title: "Hi"},
    Answer:       "Hello {{#sys.query#}}",
})
```

Or, if you've made many mutations and don't want to track each one, set
`FreshEncoding: true` on the export options to drop **all** caches at once:

```go
d.WriteFileWithOptions("out.yml", dsl.EncodeOptions{FreshEncoding: true})
```

### CLI

The bundled `cmd/export` tool exposes the same logic on the command line:

```bash
go run ./cmd/export input.yml                              # echo to stdout
go run ./cmd/export -o out.yml input.yml                   # save
go run ./cmd/export -fresh -o normalised.yml input.yml     # canonical re-encoding
go run ./cmd/export -rename-model claude-opus-4 input.yml  # transform + emit
```

## Executing a workflow (package `engine`)

Once a DSL is parsed, you can run it. The engine is a small, single-goroutine
graph traverser modelled on graphon's runtime:

```
┌─────────────────────┐
│ engine.Engine       │
│  ┌───────────────┐  │   1. seed inputs into pool
│  │ VariablePool  │  │   2. find root node (start / datasource / trigger-*)
│  └───────────────┘  │   3. lookup runner for data.type
│  ┌───────────────┐  │   4. run → commit Outputs to pool
│  │ Routing table │◀─┼── 5. follow edge for the runner-chosen sourceHandle
│  └───────────────┘  │   6. stop when reaching a RESPONSE node (end / answer)
└─────────────────────┘
```

### Quick start

```go
import (
    "context"
    "dify-dsl-parser/dsl"
    "dify-dsl-parser/engine"
)

func runWorkflow() {
    d, _ := dsl.ParseFile("workflow.yml")

    // Plug in a real or mock LLM
    llm := &engine.MockLLM{Reply: "Hello!"}

    // Optional: observe events as the workflow runs
    hooks := engine.Hooks{
        OnEvent: func(ev engine.Event) {
            switch e := ev.(type) {
            case engine.NodeFinished:
                fmt.Printf("✓ %s %s → %s\n", e.NodeType, e.NodeID, e.Handle)
            case engine.StreamChunk:
                fmt.Print(e.Delta)        // streaming LLM tokens
            }
        },
    }

    eng := engine.New(d).WithLLM(llm).WithHooks(hooks)
    res, err := eng.Run(context.Background(), engine.RunInput{
        Query:      "Tell me a joke",
        UserInputs: map[string]any{"name": "alice"},
    })
    if err != nil {
        panic(err)
    }
    fmt.Println("final outputs:", res.Outputs)
}
```

### Pluggable LLM and tool clients

The engine has zero hard dependencies on any LLM or tool infrastructure.
Implement these two interfaces to wire up your own provider:

```go
type LLMClient interface {
    Chat(ctx context.Context, req engine.LLMRequest) (*engine.LLMResponse, error)
}

type ToolClient interface {
    Invoke(ctx context.Context, req engine.ToolRequest) (*engine.ToolResponse, error)
}
```

For tests and demos, `engine.MockLLM` / `engine.MockTool` are deterministic
implementations. A real OpenAI / Anthropic adapter is a few-dozen-line
wrapper around the provider's SDK that translates `LLMRequest` → API call
→ `LLMResponse`.

### Node runners

Each DSL node type maps to one `NodeRunner`. The default registry covers:

| Node type | Runner | Notes |
|-----------|--------|-------|
| `start` | built-in | re-publishes user inputs |
| `end` | built-in | collects declared outputs |
| `answer` | built-in | renders `{{#node.var#}}` template, streams chunk |
| `llm` | built-in | dispatches via `LLMClient`, streams tokens |
| `if-else` | built-in | full operator set, sets `EdgeSourceHandle` |
| `template-transform` | built-in | tiny Jinja-lite for `{{ var.field }}` |
| `variable-aggregator` | built-in | first non-nil candidate |
| `assigner` (v1+v2) | built-in | over-write / append / extend / clear / set |
| `http-request` | built-in | net/http with template rendering |
| `tool` | built-in | dispatches via `ToolClient` |
| `iteration-start`, `loop-start`, `loop-end` | built-in | pass-through |
| _everything else_ | not registered | engine returns a clear error at runtime |

Unimplemented node types (iteration, loop, code, parameter-extractor,
question-classifier, knowledge-retrieval, agent, human-input) can be added
by writing a runner and calling `engine.RegisterRunner(typeStr, runner)`
from an `init()`. The signatures and helpers (`RenderTemplate`,
`EvaluateConditions`, `VariablePool.Get/Add`) are the same building blocks
the built-in runners use.

### Variable pool and references

The `VariablePool` mirrors graphon's runtime pool:
- two-level map: `pool[node_id][var_name] = value`
- nested-path access: `["http", "body", "items", "0", "name"]` walks into
  decoded maps and slices
- reserved namespaces: `sys`, `env`, `conversation`, `rag` (seeded by
  the engine before the first node runs)

`engine.RenderTemplate(template, pool)` performs the `{{#node.var.path#}}`
substitution used by `answer.answer`, `llm.prompt_template[].text`,
`http-request.url`, and `tool.tool_parameters[type=mixed]`.

### Engine CLI

`cmd/run` ships a self-contained demo that executes any DSL with the mock
LLM / tool, streaming each event to stdout:

```bash
go run ./cmd/run -q "say hi" testdata/basic_llm_chat_workflow.yml
go run ./cmd/run -q "hello" testdata/conditional_hello_branching_workflow.yml
go run ./cmd/run -inputs '{"switch1":1,"switch2":0}' testdata/dual_switch_variable_aggregator_workflow.yml
```

### What's intentionally missing (relative to graphon)

- **Parallelism** — graphon runs independent branches concurrently via a
  worker pool. The Go engine follows the first branch only, which is enough
  for the common linear / single-branch flows.
- **Pause / resume / abort** — graphon's command channel is not implemented;
  honour `context.Context` cancellation instead.
- **Iteration / loop containers** — the subgraph anchors (`iteration-start`,
  `loop-start`, `loop-end`) are pass-through, but the parent `iteration` /
  `loop` node runners are not implemented yet. Add them by recursively
  invoking a child engine over the subgraph.
- **Code execution** — graphon ships a sandboxed Python/JS runner; we'd
  need to either embed yaegi (Go), pull in a JS engine like goja, or
  delegate to an external sandbox.
- **Knowledge retrieval** — needs a vector-DB integration; left as a
  user-supplied runner.

## Coverage

### Top-level

- `version`, `kind`, `app` (name/mode/icon/...), `dependencies`
- `workflow.graph.{nodes, edges, viewport}`
- `workflow.features` (raw YAML, since the schema is permissive)
- `workflow.environment_variables` / `conversation_variables` / `rag_pipeline_variables`

### Node types (typed structs registered in factories)

| `data.type` | Struct |
|-------------|--------|
| `start` | `StartNodeData` |
| `end` | `EndNodeData` |
| `answer` | `AnswerNodeData` |
| `llm` | `LLMNodeData` |
| `if-else` | `IfElseNodeData` (both `cases[]` and legacy `conditions[]`) |
| `code` | `CodeNodeData` |
| `template-transform` | `TemplateTransformNodeData` |
| `question-classifier` | `QuestionClassifierNodeData` |
| `http-request` | `HTTPRequestNodeData` |
| `tool` | `ToolNodeData` (with `ToolInput.UnmarshalYAML` for legacy scalar params) |
| `variable-aggregator` | `VariableAggregatorNodeData` |
| `variable-assigner` (legacy) | `VariableAssignerNodeData` |
| `assigner` (v2) | `VariableAssignerNodeData` |
| `iteration` / `iteration-start` | `IterationNodeData`, `IterationStartNodeData` |
| `loop` / `loop-start` / `loop-end` | `LoopNodeData`, `LoopStartNodeData`, `LoopEndNodeData` |
| `parameter-extractor` | `ParameterExtractorNodeData` |
| `document-extractor` | `DocumentExtractorNodeData` |
| `list-operator` | `ListOperatorNodeData` |
| `agent` | `AgentNodeData` |
| `human-input` | `HumanInputNodeData` |
| `knowledge-retrieval` | `KnowledgeRetrievalNodeData` |
| `datasource` / `trigger-*` | `DatasourceNodeData` / `TriggerNodeData` |
| _anything else_ | `UnknownNodeData` (preserves raw YAML) |

### Variable references

- **Selectors** — structured `[]string` of length ≥ 2, with reserved namespaces
  `sys`, `env`, `conversation`, `rag`. Helpers: `IsReservedSelector`,
  `MinSelectorLength`.
- **Templates** — `{{#node.var.path#}}` syntax. Helper:
  `ExtractTemplateRefs(s)` returns every reference in a string.
- **Aggregator** — `CollectNodeReferences(node)` returns every reference a
  given node makes (across both selector fields and template strings),
  switching on the concrete `NodeData` type.

### Validation

`d.Validate()` returns a list of `Issue`s for these rules:

| Code | Meaning |
|------|---------|
| `MISSING_NODE` | An edge endpoint references an absent node |
| `INVALID_ROOT` | Graph has no root node (`start` / `datasource` / `trigger-*`) |
| `DUPLICATE_NODE_ID` | Same node ID appears more than once |
| `ITERATION_START_MISSING` | `iteration.start_node_id` doesn't point to an `iteration-start` |
| `LOOP_START_MISSING` | `loop.start_node_id` doesn't point to a `loop-start` |
| `UNKNOWN_VARIABLE_REFERENCE` | A selector / template references an unknown node |
| `SHORT_VARIABLE_SELECTOR` | A selector has fewer than 2 segments |

## DSL 0.3.x backward-compatibility

The fixtures shipped with this parser are **DSL 0.3.1** — the schema your
0.3.0 install also produces. The parser explicitly handles these legacy quirks:

1. **`retry_config`** carries an `enabled` field (instead of `retry_enabled`)
   and an `exponential_backoff` block. `RetryConfig.IsEnabled()` checks both
   names.
2. **`tool.tool_parameters`** values can be bare scalars (`{name: "value"}`)
   instead of the new `{type, value}` struct. `ToolInput.UnmarshalYAML`
   transparently wraps them as `{type: "mixed", value: ...}`, matching
   graphon's runtime conversion.
3. **`http-request.body.data`** can be a string (or empty string) instead of
   an array. `HTTPRequestBody.UnmarshalYAML` converts a non-empty string into
   a single text-typed entry, matching graphon's `Body.check_data` validator.
4. **`if-else`** accepts both the new `cases[]` form and the legacy
   `conditions[] + logical_operator` form. `IfElseNodeData.IterCases()`
   normalises across both.
5. **`memory.enabled`** field (legacy) is accepted alongside the structured
   `window` config.

## Test results

```
$ go test ./...
ok    dify-dsl-parser/dsl    0.10s

$ go test -v ./...
PASS: TestParseAllFixtures             (23 fixtures)
PASS: TestNodeTypeDispatch
PASS: TestNoUnknownNodeTypes           (23 fixtures, all node types resolved)
PASS: TestExtractTemplateRefs          (6 cases)
PASS: TestParseHTTPLineMap
PASS: TestRawDataPreserved
PASS: TestRoundTrip                    (23 fixtures, marshal → re-parse)
PASS: TestExportFreshEncoding          (mutation survives round-trip)
PASS: TestExportSetData                (full Data replacement survives round-trip)
PASS: TestExportFile                   (write to disk + re-parse)
PASS: TestExportAllFixturesRoundTrip   (23 fixtures, structure & validation match)
```

**11 top-level tests, 92 subtests (4 × 23 fixtures).**

## Design notes

- **Permissive by default** — `BaseNodeData` is embedded in every concrete
  type with `extra="allow"`-equivalent semantics: yaml.v3 silently ignores
  fields not in the struct, matching graphon's Pydantic config.
- **Raw preservation** — `Node.RawData()` returns the original `yaml.Node`
  for the data block, so callers can re-decode against their own schema if
  the registered type doesn't fit.
- **Round-trip safe** — `Node.MarshalYAML` re-emits the original raw payload
  when available, so re-exporting a parsed DSL preserves field order and
  unknown-but-valid extension fields.
- **No third-party deps beyond `gopkg.in/yaml.v3`** — easy to vendor.
