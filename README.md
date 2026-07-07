# DFlow API 层（后端）

给 `dify-dsl-parser` 加的 HTTP 服务层：把已有的 `dsl`（解析/导出）和 `engine`（执行）
两个包，包装成前端流程编辑器要用的接口——流程增删改查、发布+版本管理、导入导出
Dify DSL、以及带**逐节点执行追踪**的交互测试。

前端在每个编辑相关接口上收发的都是 **Dify DSL 本身（JSON 形式）**，和导出的 YAML 结构
完全一致，只是 JSON 而非 YAML。前端不再自造图模型，直接编辑 DSL，服务端是解析、校验、
规范化序列化的唯一真相源。

## 放到仓库的位置

把这两个目录直接拷进 `dify-dsl-parser` 仓库根目录即可（模块名 `dify-dsl-parser` 已对齐）：

```
server/            # 新增：HTTP 服务包
  convert.go       # DSL 的 JSON↔YAML 转换 + 校验封装
  store.go         # 基于文件的流程存储（草稿 + 版本快照）
  run.go           # 交互测试 SSE 端点（逐节点追踪）
  api.go           # 路由与处理器
cmd/server/
  main.go          # 入口：go run ./cmd/server
```

无新增第三方依赖，只用标准库 + 你已有的 `gopkg.in/yaml.v3`。需要 Go 1.22+（用到
`net/http` 的方法+路径路由 `GET /api/flows/{id}`）。

## 启动

```bash
go run ./cmd/server -addr :8080 -data ./data
```

默认交互测试用 `engine.MockLLM`（确定性假回复），**零配置离线可跑**。接真实模型时，
在 `cmd/server/main.go` 里把 `llm` 换成你自己实现的 `engine.LLMClient`（`engine`
包的接口，Chat 方法里调 OpenAI/Claude 等，流式回调走 `req.OnDelta`）。

## 存储布局

每个流程一个目录，DSL 存成 YAML，和 Dify 导出格式逐字节兼容（从这里导出的文件能直接
导入 Dify）：

```
data/<flowID>/meta.json          # 元数据 + 版本列表
data/<flowID>/draft.yaml         # 当前草稿
data/<flowID>/versions/1.yaml    # 已发布版本快照（不可变）
data/<flowID>/versions/2.yaml
```

草稿自由编辑，`publish` 把草稿冻结成下一个版本号——这就是和 Dify 对齐的"发布 +
版本追溯"模型。`restore` 把某个旧版本回灌到草稿即回滚。

## 接口清单

| 方法 & 路径 | 作用 |
|---|---|
| `GET /api/flows` | 流程列表（元数据，按更新时间倒序） |
| `POST /api/flows` | 新建流程 `{name, mode}`，返回带单 start 节点的空草稿 |
| `GET /api/flows/{id}` | 取流程：`{meta, graph}`，graph 是 Dify DSL 的 JSON |
| `PUT /api/flows/{id}` | 保存草稿：body 是完整 DSL JSON；校验后返回 `{ok, issues}` |
| `DELETE /api/flows/{id}` | 删除流程及全部版本 |
| `POST /api/flows/{id}/publish` | 发布草稿为新版本 `{note}` |
| `GET /api/flows/{id}/versions` | 版本列表 |
| `GET /api/flows/{id}/versions/{v}` | 取某版本的 graph（JSON） |
| `POST /api/flows/{id}/versions/{v}/restore` | 用某版本覆盖草稿（回滚） |
| `GET /api/flows/{id}/export?version=draft\|N` | 下载 YAML（Dify 格式） |
| `POST /api/flows/{id}/run` | **交互测试**：SSE 流式返回执行追踪 |
| `POST /api/dsl/import` | 传 YAML（`text` 或 `{"yaml":"..."}`），返回 graph JSON + issues（不落库） |
| `POST /api/dsl/validate` | 校验一段 DSL JSON，返回 `{valid, issues}` |

### 保存/校验返回的 issues

来自你 `dsl` 包的 `Validate()`，形如 `[{code, message, nodeId}]`。草稿即使有软校验
问题也会保存（对齐 Dify：草稿允许不完整），前端可把 issues 标注到对应节点上。

### 交互测试的 SSE 事件（前端逐帧点亮画布）

`POST /api/flows/{id}/run`，body `{query, inputs, userId}`，返回 `text/event-stream`，
每行 `data: {json}`：

| `type` | 字段 | 前端用途 |
|---|---|---|
| `node_started` | `nodeId, nodeType` | 该节点标记为"运行中"（高亮） |
| `node_finished` | `nodeId, nodeType, handle, outputs` | 标记"完成"；`handle` 是走的分支出口（if-else/分类器/错误分支），可高亮那条边 |
| `node_failed` | `nodeId, nodeType, error` | 标记"失败"，红色 |
| `chunk` | `nodeId, delta` | LLM/answer 的流式增量，实时追加到输出面板 |
| `error` | `error` | 整个运行报错 |
| `done` | `steps, finalNode, outputs` | 运行结束，终态节点与最终输出 |

这补上了你之前"交互测试没有执行到的节点追踪"的缺口——`node_started`/`node_finished`
的顺序就是执行路径，`handle` 精确到分支走向。

## 前端对接要点（React Flow）

- **画布数据**：`GET /api/flows/{id}` 的 `graph.workflow.graph.nodes/edges` 直接喂 React Flow。
  Dify 节点的 `position:{x,y}` 对应 React Flow 的 `position`；节点顶层 `type` 恒为 `custom`
  （React Flow 渲染类型），**业务类型在 `data.type`**——用它来选右侧抽屉里渲染哪个节点表单。
- **边与分支**：Dify 的 `edges[]` 是显式对象，`sourceHandle` 承载分支语义，别合并出边。
  React Flow 的多 `Handle`（id = `source`/`true`/`false`/`<case_id>`/`fail-branch` 等）
  与之一一对应。
- **保存**：把 React Flow 的 nodes/edges 写回 `graph.workflow.graph`，整个 DSL JSON `PUT` 回来。
- **导入导出界面**：导出直接 `window.open(export URL)` 下载；导入把文件文本 `POST` 到
  `/api/dsl/import`，拿回 graph 渲染，再 `POST /api/flows` + `PUT` 落库。

## 已验证

`server/` + `cmd/server` 干净编译、`go vet` 通过，并端到端实跑：新建 → 保存 start→llm→answer
草稿 → 发布 v1/v2 → 版本列表 → 导出 Dify 格式 YAML → 交互测试拿到逐节点 SSE 追踪。
