package dsl

import (
	"fmt"
	"gopkg.in/yaml.v3"
)

// Node represents a single node entry in graph.nodes.
//
// React-Flow specific fields (position, width, ...) are kept verbatim because
// they round-trip through Dify when re-exporting the DSL. The polymorphic
// business payload lives in the Data field.
//
// Reference:
//   - graphon/src/graphon/entities/graph_config.py: NodeConfigDict
//   - graphon/src/graphon/graph/graph.py: Graph._parse_node_configs
type Node struct {
	// ID is the node identifier (also used as the first segment of variable
	// selectors that reference this node's outputs).
	ID string `yaml:"id"`

	// Type is the React-Flow rendering type. Common values:
	//   - "custom"
	//   - "custom-iteration-start"
	//   - "custom-loop-start"
	//   - "custom-note"   (canvas annotation, has no business data)
	// The business node type is *not* this field; it lives in Data.NodeType().
	Type string `yaml:"type,omitempty"`

	// Position / dimensions are React-Flow layout state.
	Position         *yaml.Node `yaml:"position,omitempty"`
	PositionAbsolute *yaml.Node `yaml:"positionAbsolute,omitempty"`
	Width            int        `yaml:"width,omitempty"`
	Height           int        `yaml:"height,omitempty"`
	SourcePosition   string     `yaml:"sourcePosition,omitempty"`
	TargetPosition   string     `yaml:"targetPosition,omitempty"`
	Selected         bool       `yaml:"selected,omitempty"`
	Dragging         bool       `yaml:"dragging,omitempty"`
	Draggable        *bool      `yaml:"draggable,omitempty"`
	Selectable       *bool      `yaml:"selectable,omitempty"`
	Deletable        *bool      `yaml:"deletable,omitempty"`
	ZIndex           *int       `yaml:"zIndex,omitempty"`
	ParentID         string     `yaml:"parentId,omitempty"`
	Extent           string     `yaml:"extent,omitempty"`

	// Data is the polymorphic business payload, decoded based on data.type.
	// It is never nil after a successful parse; unknown types yield
	// *UnknownNodeData so callers can still inspect them.
	Data NodeData `yaml:"-"`

	// rawData keeps the original YAML node for re-serialization or
	// re-decoding. It is populated automatically by UnmarshalYAML.
	rawData *yaml.Node `yaml:"-"`
}

// IsNote reports whether this node is a canvas annotation that should be
// skipped during execution-graph processing (matches Graph._filter_canvas_only_nodes).
func (n Node) IsNote() bool {
	return n.Type == "custom-note"
}

// RawData returns the original yaml.Node for the data block, useful for
// callers that want to re-decode against a custom struct.
func (n Node) RawData() *yaml.Node {
	return n.rawData
}

// UnmarshalYAML implements the two-phase decoder.
//
// Phase 1: decode all React-Flow fields plus the data block as a raw yaml.Node.
// Phase 2: peek at data.type, then decode the raw data into a concrete NodeData
//
//	struct via the registry.
func (n *Node) UnmarshalYAML(value *yaml.Node) error {
	// Use a private alias to avoid recursion into Node.UnmarshalYAML.
	type rawNode struct {
		ID               string     `yaml:"id"`
		Type             string     `yaml:"type,omitempty"`
		Position         *yaml.Node `yaml:"position,omitempty"`
		PositionAbsolute *yaml.Node `yaml:"positionAbsolute,omitempty"`
		Width            int        `yaml:"width,omitempty"`
		Height           int        `yaml:"height,omitempty"`
		SourcePosition   string     `yaml:"sourcePosition,omitempty"`
		TargetPosition   string     `yaml:"targetPosition,omitempty"`
		Selected         bool       `yaml:"selected,omitempty"`
		Dragging         bool       `yaml:"dragging,omitempty"`
		Draggable        *bool      `yaml:"draggable,omitempty"`
		Selectable       *bool      `yaml:"selectable,omitempty"`
		Deletable        *bool      `yaml:"deletable,omitempty"`
		ZIndex           *int       `yaml:"zIndex,omitempty"`
		ParentID         string     `yaml:"parentId,omitempty"`
		Extent           string     `yaml:"extent,omitempty"`
		Data             yaml.Node  `yaml:"data"`
	}

	var raw rawNode
	if err := value.Decode(&raw); err != nil {
		return fmt.Errorf("node phase-1 decode: %w", err)
	}

	n.ID = raw.ID
	n.Type = raw.Type
	n.Position = raw.Position
	n.PositionAbsolute = raw.PositionAbsolute
	n.Width = raw.Width
	n.Height = raw.Height
	n.SourcePosition = raw.SourcePosition
	n.TargetPosition = raw.TargetPosition
	n.Selected = raw.Selected
	n.Dragging = raw.Dragging
	n.Draggable = raw.Draggable
	n.Selectable = raw.Selectable
	n.Deletable = raw.Deletable
	n.ZIndex = raw.ZIndex
	n.ParentID = raw.ParentID
	n.Extent = raw.Extent
	n.rawData = &raw.Data

	// Custom-note nodes have no business payload.
	if n.IsNote() {
		n.Data = &NoteNodeData{}
		return nil
	}

	// If data is missing or empty, return an UnknownNodeData rather than failing.
	if raw.Data.Kind == 0 {
		n.Data = &UnknownNodeData{}
		return nil
	}

	// Phase 2: peek at data.type to dispatch.
	var meta struct {
		Type    string `yaml:"type"`
		Title   string `yaml:"title,omitempty"`
		Version string `yaml:"version,omitempty"`
	}
	// Decode errors at this level are non-fatal: we can still surface the raw
	// payload as UnknownNodeData. This matches BaseNodeData(extra="allow").
	_ = raw.Data.Decode(&meta)

	// Special note: graphon's BuiltinNodeTypes uses "variable-assigner" for the
	// legacy aggregator (deprecated) and "assigner" for the v2 variable
	// assigner. We dispatch by the explicit type string, but the registry below
	// also handles versioned node-data variants where needed.
	data := decodeNodeData(meta.Type, meta.Version, &raw.Data)
	n.Data = data
	return nil
}

// MarshalYAML implements yaml.Marshaler for round-trip use cases.
// It re-emits all React-Flow fields plus the original data payload.
//
// Strategy:
//   - If the original raw yaml.Node is still attached (no caller has called
//     SetData / MarkDataDirty), re-emit it verbatim. This preserves field
//     order and any unknown-but-valid fields.
//   - Otherwise, encode the typed Data struct fresh.
func (n Node) MarshalYAML() (any, error) {
	dataNode, err := n.dataAsYAMLNode()
	if err != nil {
		return nil, err
	}
	type out struct {
		ID               string     `yaml:"id"`
		Type             string     `yaml:"type,omitempty"`
		Position         *yaml.Node `yaml:"position,omitempty"`
		PositionAbsolute *yaml.Node `yaml:"positionAbsolute,omitempty"`
		Width            int        `yaml:"width,omitempty"`
		Height           int        `yaml:"height,omitempty"`
		SourcePosition   string     `yaml:"sourcePosition,omitempty"`
		TargetPosition   string     `yaml:"targetPosition,omitempty"`
		Selected         bool       `yaml:"selected,omitempty"`
		ParentID         string     `yaml:"parentId,omitempty"`
		Extent           string     `yaml:"extent,omitempty"`
		Data             *yaml.Node `yaml:"data"`
	}
	return out{
		ID: n.ID, Type: n.Type, Position: n.Position, PositionAbsolute: n.PositionAbsolute,
		Width: n.Width, Height: n.Height,
		SourcePosition: n.SourcePosition, TargetPosition: n.TargetPosition,
		Selected: n.Selected, ParentID: n.ParentID, Extent: n.Extent,
		Data: dataNode,
	}, nil
}

func (n Node) dataAsYAMLNode() (*yaml.Node, error) {
	// Re-use the original raw yaml.Node when available (preserves field order
	// and unknown fields). Otherwise marshal the typed struct.
	if n.rawData != nil && n.rawData.Kind != 0 {
		return n.rawData, nil
	}
	out := &yaml.Node{}
	if err := out.Encode(n.Data); err != nil {
		return nil, fmt.Errorf("encode node.data: %w", err)
	}
	return out, nil
}

// SetData replaces the typed Data and marks the cached raw payload as stale,
// so the next MarshalYAML re-encodes from the typed struct.
//
// Use this when you've assigned an entirely new Data value:
//
//	n.SetData(&dsl.LLMNodeData{ ... })
func (n *Node) SetData(d NodeData) {
	n.Data = d
	n.rawData = nil
}

// MarkDataDirty drops the cached raw payload without changing the Data
// pointer, so the next MarshalYAML re-encodes from the typed struct.
//
// Use this when you've mutated fields *through* the existing typed pointer:
//
//	llm := n.Data.(*dsl.LLMNodeData)
//	llm.Model.Name = "gpt-4o"
//	n.MarkDataDirty()
func (n *Node) MarkDataDirty() {
	n.rawData = nil
}
