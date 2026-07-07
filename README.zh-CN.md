-# Dify DSL 解析器与执行引擎(Go)

一个纯 Go 实现的 [Dify](https://github.com/langgenius/dify) 工作流 DSL 文件工具集:

1. **解析器**(包 `dsl`)—— 强类型的两阶段多态解码器,支持 15+ 种节点类型,基于 23 个真实的 Dify 测试样例验证。
2. **导出器**(`dsl.Marshal/Encode/WriteFile`)—— 将解析后的 DSL 反向序列化为 YAML,支持原样保留与全新编码两种模式。
3. **引擎**(包 `engine`)—— 单 goroutine 执行器,遍历已解析的图、求值条件、调用插件式 LLM/Tool 客户端并流式产生事件。在设计上对齐 [graphon](https://github.com/langgenius/graphon) 的运行时模型(变量池 + 每节点 `Run()` + 边 handle 路由),但不包含 graphon 的 worker pool 层。

## 为什么需要两阶段解码?

Dify DSL 节点的形态如下:

```yaml
- id: my_node
  type: custom               # ← React Flow 渲染类型,而非业务类型
  data:
    type: llm                # ← 业务类型在这里
    model: { ... }           # ← schema 取决于 data.type
    prompt_template: [...]
```

每个 `data.type` 对应完全不同的 schema。Go 没有原生的可辨识联合(discriminated union),所以我们的做法是:

1. **阶段 1** —— 将每个节点的 React-Flow 外壳以及 `data` 子树解码为通用的 `yaml.Node`。
2. **阶段 2** —— 查看 `data.type`,从注册表中查到对应的工厂函数,把原始的 `yaml.Node` 解码为具体的 `*XxxNodeData` 结构体。

未知类型会回退到 `*UnknownNodeData`,因此一个新的扩展类型不会让整个 DSL 解析失败。

## 目录结构

```
dify-dsl-parser/
├── go.mod
├── cmd/
│   ├── parse/              # CLI:任意 DSL 文件的结构化摘要
│   ├── inspect/            # CLI:对单个 DSL 进行深度分析(变量引用等)
│   └── export/             # CLI:把已解析 DSL 重新输出为 YAML,可选转换
├── dsl/
│   ├── dsl.go              # 顶层:DSL / AppMeta / Dependency / Parse()
│   ├── workflow.go         # Workflow / Graph / Edge / ScopedVariable
│   ├── node.go             # Node + UnmarshalYAML/MarshalYAML 两阶段解码器
│   ├── base.go             # BaseNodeData、NodeData 接口、类型常量、注册表
│   ├── export.go           # DSL.Marshal / WriteTo / WriteFile + EncodeOptions
│   ├── nodes_io.go         # start、end、answer
│   ├── nodes_llm.go        # llm、question-classifier、parameter-extractor、knowledge-retrieval
│   ├── nodes_logic.go      # if-else(cases 与旧版 conditions)
│   ├── nodes_compute.go    # code、template-transform、document-extractor
│   ├── nodes_http.go       # http-request、tool(包含旧版 ToolInput / HTTPRequestBody)
│   ├── nodes_data.go       # variable-aggregator、assigner(v1+v2)、list-operator
│   ├── nodes_loop.go       # iteration、iteration-start、loop、loop-start、loop-end
│   ├── nodes_other.go      # agent、human-input、trigger-*、datasource
│   ├── selectors.go        # ExtractTemplateRefs、ParseHTTPLineMap、命名空间
│   ├── validate.go         # 图级别校验:边、根节点、子图、变量引用
│   └── parser_test.go      # 11 个测试,23 个样例,完整往返与导出覆盖
└── testdata/               # 23 个真实 DSL 文件(DSL 版本 0.3.1)
```

## 快速开始

```bash
# 构建
go build ./...

# 查看 DSL 文件摘要
go run ./cmd/parse testdata/basic_llm_chat_workflow.yml

# 深度检查:列出每个节点的结构与变量引用
go run ./cmd/inspect testdata/http_request_with_json_tool_workflow.yml

# 运行所有测试
go test ./...
```

## 编程式使用

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
            // 对类型化的 Data 做 switch
            switch nd := n.Data.(type) {
            case *dsl.LLMNodeData:
                fmt.Printf("LLM %s: %s/%s\n", n.ID, nd.Model.Provider, nd.Model.Name)
                if msgs, ok := nd.PromptMessages(); ok {
                    fmt.Printf("  %d 条聊天消息\n", len(msgs))
                }
            case *dsl.IfElseNodeData:
                for _, c := range nd.IterCases() {
                    fmt.Printf("if-else %s: case %s, %d 个条件\n",
                        n.ID, c.CaseID, len(c.Conditions))
                }
            case *dsl.HTTPRequestNodeData:
                hdrs := dsl.ParseHTTPLineMap(nd.Headers)
                fmt.Printf("HTTP %s: %s %s, %d 个请求头\n",
                    n.ID, nd.Method, nd.URL, len(hdrs))
            }

            // 静态分析:节点内的所有变量引用
            for _, ref := range dsl.CollectNodeReferences(&n) {
                fmt.Printf("  %s -> %v (raw=%s)\n", n.ID, ref.Selector, ref.Raw)
            }
        }
    }

    // 图级别校验
    for _, issue := range d.Validate() {
        fmt.Println("ISSUE:", issue)
    }
}
```

## 导出回 YAML

已解析的 DSL 可以重新输出为 YAML,提供两种编码策略:

| 模式 | 适用场景 | 行为 |
|------|----------|------|
| **Verbatim**(默认) | 往返未修改的 DSL,或规范化文件结构 | 每个节点的 `data` 块基于缓存的原始 `yaml.Node` 原样输出 —— 字段顺序及未知字段都保持不变。顶层键遵循 Dify 官方顺序(`version`、`kind`、`app`、`workflow`、`dependencies`)。 |
| **Fresh** | 修改了类型化字段后,或希望产出规范化文件 | 丢弃缓存的原始数据。每个节点都从 `*XxxNodeData` 结构体重新编码,字段顺序遵循 Go 结构体定义,`omitempty` 的零值会被省略。 |

### API

```go
// 默认:verbatim,缩进为 2
b, err := d.Marshal()                // []byte
err   := d.Encode(os.Stdout)         // io.Writer
err   := d.WriteFile("out.yml")      // 文件

// 自定义选项
opts := dsl.EncodeOptions{Indent: 4, FreshEncoding: true}
b, err := d.MarshalWithOptions(opts)
err   := d.EncodeWithOptions(os.Stdout, opts)
err   := d.WriteFileWithOptions("out.yml", opts)
```

### 在导出前修改类型化字段

如果你**通过现有的类型化指针**修改字段,需要把节点标记为脏,以便下次导出时从类型化结构体重新编码:

```go
for i := range d.Workflow.Graph.Nodes {
    n := &d.Workflow.Graph.Nodes[i]
    if llm, ok := n.Data.(*dsl.LLMNodeData); ok {
        llm.Model.Name = "claude-opus-4"
        n.MarkDataDirty()           // ← 重要
    }
}
d.WriteFile("updated.yml")
```

如果你用全新构造的结构体替换了整个 `Data`,使用 `SetData`(它会自动清掉缓存):

```go
n.SetData(&dsl.AnswerNodeData{
    BaseNodeData: dsl.BaseNodeData{Type: dsl.NodeTypeAnswer, Title: "Hi"},
    Answer:       "Hello {{#sys.query#}}",
})
```

或者,如果你做了大量修改且不想逐个跟踪,在导出选项中设置 `FreshEncoding: true` 一次性丢弃**所有**缓存:

```go
d.WriteFileWithOptions("out.yml", dsl.EncodeOptions{FreshEncoding: true})
```

### CLI

自带的 `cmd/export` 工具在命令行上提供同样的能力:

```bash
go run ./cmd/export input.yml                              # 输出到 stdout
go run ./cmd/export -o out.yml input.yml                   # 写入文件
go run ./cmd/export -fresh -o normalised.yml input.yml     # 规范化重新编码
go run ./cmd/export -rename-model claude-opus-4 input.yml  # 转换后输出
```

## 执行工作流(包 `engine`)

DSL 解析完后,你就可以运行它。引擎是一个小巧的、单 goroutine 的图遍历器,设计上对齐 graphon 的运行时:

```
┌─────────────────────┐
│ engine.Engine       │
│  ┌───────────────┐  │   1. 把输入注入变量池
│  │ VariablePool  │  │   2. 找到根节点(start / datasource / trigger-*)
│  └───────────────┘  │   3. 根据 data.type 查找 runner
│  ┌───────────────┐  │   4. 运行 → 把 Outputs 提交到变量池
│  │ Routing table │◀─┼── 5. 沿着 runner 选择的 sourceHandle 走对应的边
│  └───────────────┘  │   6. 抵达 RESPONSE 节点(end / answer)时停止
└─────────────────────┘
```

### 快速上手

```go
import (
    "context"
    "dify-dsl-parser/dsl"
    "dify-dsl-parser/engine"
)

func runWorkflow() {
    d, _ := dsl.ParseFile("workflow.yml")

    // 接入真实或 mock 的 LLM
    llm := &engine.MockLLM{Reply: "Hello!"}

    // 可选:在工作流运行时观察事件
    hooks := engine.Hooks{
        OnEvent: func(ev engine.Event) {
            switch e := ev.(type) {
            case engine.NodeFinished:
                fmt.Printf("✓ %s %s → %s\n", e.NodeType, e.NodeID, e.Handle)
            case engine.StreamChunk:
                fmt.Print(e.Delta)        // 流式 LLM token
            }
        },
    }

    eng := engine.New(d).WithLLM(llm).WithHooks(hooks)
    res, err := eng.Run(context.Background(), engine.RunInput{
        Query:      "讲个笑话",
        UserInputs: map[string]any{"name": "alice"},
    })
    if err != nil {
        panic(err)
    }
    fmt.Println("最终输出:", res.Outputs)
}
```

### 可插拔的 LLM 与 Tool 客户端

引擎对任何 LLM 或工具基础设施都没有硬依赖。实现以下两个接口即可接入你自己的提供方:

```go
type LLMClient interface {
    Chat(ctx context.Context, req engine.LLMRequest) (*engine.LLMResponse, error)
}

type ToolClient interface {
    Invoke(ctx context.Context, req engine.ToolRequest) (*engine.ToolResponse, error)
}
```

为了方便测试和演示,`engine.MockLLM` / `engine.MockTool` 提供了确定性实现。一个真实的 OpenAI / Anthropic 适配器,只需几十行代码就能完成 `LLMRequest` → API 调用 → `LLMResponse` 的转换。

### 节点 runner

每种 DSL 节点类型对应一个 `NodeRunner`。默认注册表覆盖如下:

| 节点类型 | Runner | 备注 |
|----------|--------|------|
| `start` | 内置 | 重新发布用户输入 |
| `end` | 内置 | 收集声明的 outputs |
| `answer` | 内置 | 渲染 `{{#node.var#}}` 模板,流式输出 |
| `llm` | 内置 | 通过 `LLMClient` 派发,流式 token |
| `if-else` | 内置 | 完整的运算符集合,设置 `EdgeSourceHandle` |
| `template-transform` | 内置 | 极简的 Jinja-lite,用于 `{{ var.field }}` |
| `variable-aggregator` | 内置 | 取首个非 nil 候选项 |
| `assigner`(v1+v2) | 内置 | over-write / append / extend / clear / set |
| `http-request` | 内置 | net/http,带模板渲染 |
| `tool` | 内置 | 通过 `ToolClient` 派发 |
| `iteration-start`、`loop-start`、`loop-end` | 内置 | 透传 |
| _其他_ | 未注册 | 引擎在运行时返回明确错误 |

未实现的节点类型(iteration、loop、code、parameter-extractor、question-classifier、knowledge-retrieval、agent、human-input)可以通过编写 runner、在 `init()` 中调用 `engine.RegisterRunner(typeStr, runner)` 来添加。函数签名与辅助方法(`RenderTemplate`、`EvaluateConditions`、`VariablePool.Get/Add`)与内置 runner 相同。

### 变量池与引用

`VariablePool` 镜像了 graphon 的运行时变量池:
- 两层 map:`pool[node_id][var_name] = value`
- 支持嵌套路径访问:`["http", "body", "items", "0", "name"]` 会在 map 与 slice 中递归取值
- 保留命名空间:`sys`、`env`、`conversation`、`rag`(由引擎在第一个节点运行前预先填充)

`engine.RenderTemplate(template, pool)` 实现 `{{#node.var.path#}}` 替换,被 `answer.answer`、`llm.prompt_template[].text`、`http-request.url`、`tool.tool_parameters[type=mixed]` 使用。

### 引擎 CLI

`cmd/run` 提供一个自包含的演示程序,可以用 mock LLM / tool 执行任意 DSL,把每个事件流式输出到 stdout:

```bash
go run ./cmd/run -q "say hi" testdata/basic_llm_chat_workflow.yml
go run ./cmd/run -q "hello" testdata/conditional_hello_branching_workflow.yml
go run ./cmd/run -inputs '{"switch1":1,"switch2":0}' testdata/dual_switch_variable_aggregator_workflow.yml
```

### 相比 graphon 有意省略的部分

- **并行** —— graphon 通过 worker pool 并发执行独立分支。Go 引擎只走第一条分支,这对常见的线性 / 单分支流程已经够用。
- **暂停 / 恢复 / 中止** —— 没有实现 graphon 的命令通道;改为通过 `context.Context` 取消来响应。
- **iteration / loop 容器** —— 子图锚点(`iteration-start`、`loop-start`、`loop-end`)是透传节点,但父级 `iteration` / `loop` 节点的 runner 还没实现。可以通过递归调用一个子引擎来在子图上执行。
- **代码执行** —— graphon 自带沙箱化的 Python/JS runner;实现的话需要嵌入 yaegi(Go)、引入 goja 这类 JS 引擎,或者代理到外部沙箱。
- **知识检索** —— 需要向量数据库集成;留给用户提供自定义 runner。

## 覆盖范围

### 顶层

- `version`、`kind`、`app`(name/mode/icon/...)、`dependencies`
- `workflow.graph.{nodes, edges, viewport}`
- `workflow.features`(原始 YAML,因为该 schema 比较宽松)
- `workflow.environment_variables` / `conversation_variables` / `rag_pipeline_variables`

### 节点类型(已在工厂中注册的类型化结构体)

| `data.type` | 结构体 |
|-------------|--------|
| `start` | `StartNodeData` |
| `end` | `EndNodeData` |
| `answer` | `AnswerNodeData` |
| `llm` | `LLMNodeData` |
| `if-else` | `IfElseNodeData`(同时支持 `cases[]` 与旧版 `conditions[]`) |
| `code` | `CodeNodeData` |
| `template-transform` | `TemplateTransformNodeData` |
| `question-classifier` | `QuestionClassifierNodeData` |
| `http-request` | `HTTPRequestNodeData` |
| `tool` | `ToolNodeData`(`ToolInput.UnmarshalYAML` 兼容旧版标量参数) |
| `variable-aggregator` | `VariableAggregatorNodeData` |
| `variable-assigner`(旧版) | `VariableAssignerNodeData` |
| `assigner`(v2) | `VariableAssignerNodeData` |
| `iteration` / `iteration-start` | `IterationNodeData`、`IterationStartNodeData` |
| `loop` / `loop-start` / `loop-end` | `LoopNodeData`、`LoopStartNodeData`、`LoopEndNodeData` |
| `parameter-extractor` | `ParameterExtractorNodeData` |
| `document-extractor` | `DocumentExtractorNodeData` |
| `list-operator` | `ListOperatorNodeData` |
| `agent` | `AgentNodeData` |
| `human-input` | `HumanInputNodeData` |
| `knowledge-retrieval` | `KnowledgeRetrievalNodeData` |
| `datasource` / `trigger-*` | `DatasourceNodeData` / `TriggerNodeData` |
| _其他_ | `UnknownNodeData`(保留原始 YAML) |

### 变量引用

- **Selectors** —— 长度 ≥ 2 的结构化 `[]string`,保留命名空间为 `sys`、`env`、`conversation`、`rag`。辅助函数:`IsReservedSelector`、`MinSelectorLength`。
- **Templates** —— `{{#node.var.path#}}` 语法。辅助函数:`ExtractTemplateRefs(s)` 返回字符串内的所有引用。
- **聚合** —— `CollectNodeReferences(node)` 返回某个节点产生的所有引用(同时覆盖 selector 字段与模板字符串),根据具体的 `NodeData` 类型分发。

### 校验

`d.Validate()` 针对以下规则返回 `Issue` 列表:

| 错误码 | 含义 |
|--------|------|
| `MISSING_NODE` | 边的端点指向不存在的节点 |
| `INVALID_ROOT` | 图没有根节点(`start` / `datasource` / `trigger-*`) |
| `DUPLICATE_NODE_ID` | 同一个节点 ID 出现多次 |
| `ITERATION_START_MISSING` | `iteration.start_node_id` 没有指向 `iteration-start` |
| `LOOP_START_MISSING` | `loop.start_node_id` 没有指向 `loop-start` |
| `UNKNOWN_VARIABLE_REFERENCE` | selector / 模板引用了未知节点 |
| `SHORT_VARIABLE_SELECTOR` | selector 段数不足 2 |

## DSL 0.3.x 向后兼容

本仓库携带的样例是 **DSL 0.3.1** —— 这也是你 0.3.0 安装版本生成的 schema。解析器显式处理了以下旧版怪癖:

1. **`retry_config`** 使用 `enabled` 字段(而不是 `retry_enabled`),并带有 `exponential_backoff` 块。`RetryConfig.IsEnabled()` 同时检查这两种命名。
2. **`tool.tool_parameters`** 的值可以是裸标量(`{name: "value"}`)而不是新版的 `{type, value}` 结构。`ToolInput.UnmarshalYAML` 透明地把它们包装为 `{type: "mixed", value: ...}`,与 graphon 运行时的转换行为一致。
3. **`http-request.body.data`** 可以是字符串(或空字符串)而不是数组。`HTTPRequestBody.UnmarshalYAML` 会把非空字符串转换为单个 text 类型条目,与 graphon 的 `Body.check_data` 校验器一致。
4. **`if-else`** 同时接受新版 `cases[]` 与旧版 `conditions[] + logical_operator`。`IfElseNodeData.IterCases()` 把两者归一化。
5. **`memory.enabled`** 字段(旧版)与结构化的 `window` 配置一同被接受。

## 测试结果

```
$ go test ./...
ok    dify-dsl-parser/dsl    0.10s

$ go test -v ./...
PASS: TestParseAllFixtures             (23 个样例)
PASS: TestNodeTypeDispatch
PASS: TestNoUnknownNodeTypes           (23 个样例,所有节点类型都已识别)
PASS: TestExtractTemplateRefs          (6 个用例)
PASS: TestParseHTTPLineMap
PASS: TestRawDataPreserved
PASS: TestRoundTrip                    (23 个样例,marshal → 重新解析)
PASS: TestExportFreshEncoding          (修改可在往返中保留)
PASS: TestExportSetData                (整个 Data 替换可在往返中保留)
PASS: TestExportFile                   (写入磁盘 + 重新解析)
PASS: TestExportAllFixturesRoundTrip   (23 个样例,结构与校验都吻合)
```

**11 个顶层测试,92 个子测试(4 × 23 个样例)。**

## 设计要点

- **默认宽松** —— `BaseNodeData` 嵌入到每个具体类型中,语义等价于 `extra="allow"`:yaml.v3 会静默忽略结构体之外的字段,与 graphon 的 Pydantic 配置一致。
- **保留原始数据** —— `Node.RawData()` 返回 data 块的原始 `yaml.Node`,因此调用者可以基于自己的 schema 重新解码,以备已注册类型不适用的情况。
- **往返安全** —— `Node.MarshalYAML` 在原始 payload 可用时直接重发,因此重新导出已解析的 DSL 能保留字段顺序以及未知但合法的扩展字段。
- **除 `gopkg.in/yaml.v3` 外没有第三方依赖** —— 易于 vendor。