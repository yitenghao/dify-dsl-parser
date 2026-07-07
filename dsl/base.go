package dsl

import "gopkg.in/yaml.v3"

// NodeType is the business node type string (data.type).
//
// Reference: graphon/src/graphon/enums.py:BuiltinNodeTypes
type NodeType = string

// Built-in node type constants.
const (
	NodeTypeStart              NodeType = "start"
	NodeTypeEnd                NodeType = "end"
	NodeTypeAnswer             NodeType = "answer"
	NodeTypeLLM                NodeType = "llm"
	NodeTypeKnowledgeRetrieval NodeType = "knowledge-retrieval"
	NodeTypeIfElse             NodeType = "if-else"
	NodeTypeCode               NodeType = "code"
	NodeTypeTemplateTransform  NodeType = "template-transform"
	NodeTypeQuestionClassifier NodeType = "question-classifier"
	NodeTypeHTTPRequest        NodeType = "http-request"
	NodeTypeTool               NodeType = "tool"
	NodeTypeDatasource         NodeType = "datasource"
	NodeTypeVariableAggregator NodeType = "variable-aggregator"
	// "variable-assigner" was the legacy alias for variable-aggregator;
	// in DSL >= 0.5 it is reused for the legacy v1 assigner.
	NodeTypeLegacyAggregator   NodeType = "variable-assigner"
	NodeTypeLoop               NodeType = "loop"
	NodeTypeLoopStart          NodeType = "loop-start"
	NodeTypeLoopEnd            NodeType = "loop-end"
	NodeTypeIteration          NodeType = "iteration"
	NodeTypeIterationStart     NodeType = "iteration-start"
	NodeTypeParameterExtractor NodeType = "parameter-extractor"
	NodeTypeAssigner           NodeType = "assigner" // v2 variable assigner
	NodeTypeDocumentExtractor  NodeType = "document-extractor"
	NodeTypeListOperator       NodeType = "list-operator"
	NodeTypeAgent              NodeType = "agent"
	NodeTypeHumanInput         NodeType = "human-input"
	// Trigger nodes (rag-pipeline only). Defined in dify, not graphon.
	NodeTypeTriggerWebhook  NodeType = "trigger-webhook"
	NodeTypeTriggerSchedule NodeType = "trigger-schedule"
	NodeTypeTriggerPlugin   NodeType = "trigger-plugin"
)

// ErrorStrategy values.
const (
	ErrorStrategyFailBranch   = "fail-branch"
	ErrorStrategyDefaultValue = "default-value"
)

// FailBranchSourceHandle values.
const (
	FailBranchHandleFailure = "fail-branch"
	FailBranchHandleSuccess = "success-branch"
)

// NodeData is the interface every concrete *NodeData struct implements.
//
// Implementations embed BaseNodeData by value (so the shared fields are
// accessible via the Base() accessor). They also report their own NodeType.
type NodeData interface {
	// NodeType returns the business type string from data.type.
	NodeType() NodeType
	// Base returns a pointer to the embedded BaseNodeData for shared access.
	Base() *BaseNodeData
}

// BaseNodeData holds fields shared by every node type.
//
// Reference: graphon/src/graphon/entities/base_node_data.py
type BaseNodeData struct {
	Type          NodeType       `yaml:"type"`
	Title         string         `yaml:"title,omitempty"`
	Desc          string         `yaml:"desc,omitempty"`
	Version       string         `yaml:"version,omitempty"`
	ErrorStrategy string         `yaml:"error_strategy,omitempty"`
	DefaultValue  []DefaultValue `yaml:"default_value,omitempty"`
	RetryConfig   *RetryConfig   `yaml:"retry_config,omitempty"`

	// Selected and IsInIteration / IsInLoop are UI fields that some node
	// payloads carry. They're not used by the engine but they are persisted.
	Selected      bool `yaml:"selected,omitempty"`
	IsInIteration bool `yaml:"isInIteration,omitempty"`
	IsInLoop      bool `yaml:"isInLoop,omitempty"`
}

// NodeType allows BaseNodeData itself to satisfy NodeData when needed.
func (b *BaseNodeData) NodeType() NodeType { return b.Type }

// Base returns a pointer to itself, which is how concrete NodeData types
// expose the embedded base block.
func (b *BaseNodeData) Base() *BaseNodeData { return b }

// RetryConfig is the per-node retry policy.
//
// The DSL has used multiple shapes for this block:
//   - DSL >= 0.5 : {retry_enabled, max_retries, retry_interval}
//   - DSL <= 0.4 : {enabled, max_retries, retry_interval, exponential_backoff: {...}}
//
// We keep both shapes so old fixtures parse cleanly.
type RetryConfig struct {
	// Retry switch. Either retry_enabled (new) or enabled (legacy) sets this.
	RetryEnabled       bool       `yaml:"retry_enabled,omitempty"`
	Enabled            bool       `yaml:"enabled,omitempty"`
	MaxRetries         int        `yaml:"max_retries,omitempty"`
	RetryInterval      int        `yaml:"retry_interval,omitempty"` // milliseconds
	ExponentialBackoff *yaml.Node `yaml:"exponential_backoff,omitempty"`
}

// IsEnabled returns true if either the new or legacy enable flag is set.
func (r *RetryConfig) IsEnabled() bool {
	if r == nil {
		return false
	}
	return r.RetryEnabled || r.Enabled
}

// DefaultValue is one entry of BaseNodeData.DefaultValue.
type DefaultValue struct {
	Key   string `yaml:"key"`
	Type  string `yaml:"type"`
	Value any    `yaml:"value"`
}

// UnknownNodeData is returned when the data.type is not one of the
// registered builtin types. It preserves the raw YAML node for inspection.
type UnknownNodeData struct {
	BaseNodeData `yaml:",inline"`
	// Raw is the original yaml.Node for the data block.
	Raw *yaml.Node `yaml:"-"`
}

// MarshalYAML re-emits the original raw YAML node when present so unknown /
// extension types round-trip without losing fields. Falls back to the
// embedded BaseNodeData fields when no raw is available.
func (u *UnknownNodeData) MarshalYAML() (any, error) {
	if u.Raw != nil && u.Raw.Kind != 0 {
		return u.Raw, nil
	}
	return u.BaseNodeData, nil
}

// NoteNodeData is the empty business payload for a "custom-note" canvas node.
type NoteNodeData struct {
	BaseNodeData `yaml:",inline"`
}

// ----------------------------------------------------------------------------
// Registry: type-string -> factory
// ----------------------------------------------------------------------------

// nodeFactory builds a fresh NodeData ready to receive a yaml.Decode call.
type nodeFactory func() NodeData

var nodeRegistry = map[NodeType]nodeFactory{}

// registerNodeType is called by individual node files via init() to register
// their concrete factory.
func registerNodeType(t NodeType, f nodeFactory) {
	if _, ok := nodeRegistry[t]; ok {
		panic("dsl: duplicate node type registration: " + t)
	}
	nodeRegistry[t] = f
}

// decodeNodeData is the central dispatch invoked by Node.UnmarshalYAML.
//
// It returns a fully-decoded NodeData. Failures are turned into UnknownNodeData
// so a single broken node cannot abort an entire DSL parse.
func decodeNodeData(typ, _version string, raw *yaml.Node) NodeData {
	if raw == nil || raw.Kind == 0 {
		return &UnknownNodeData{}
	}
	factory, ok := nodeRegistry[typ]
	if !ok {
		// Unknown type: keep the raw payload and the few BaseNodeData fields
		// we can recover.
		u := &UnknownNodeData{Raw: raw}
		_ = raw.Decode(&u.BaseNodeData)
		return u
	}
	d := factory()
	if err := raw.Decode(d); err != nil {
		// Decoding failed: degrade gracefully to UnknownNodeData but keep the
		// original yaml.Node so downstream callers can still inspect it.
		u := &UnknownNodeData{Raw: raw}
		_ = raw.Decode(&u.BaseNodeData)
		return u
	}
	return d
}
