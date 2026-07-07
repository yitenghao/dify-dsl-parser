package dsl

import "gopkg.in/yaml.v3"

// ----------------------------------------------------------------------------
// variable-aggregator
// ----------------------------------------------------------------------------

// VariableAggregatorNodeData is the payload for the "variable-aggregator" node.
//
// Reference: graphon/src/graphon/nodes/variable_aggregator/entities.py
type VariableAggregatorNodeData struct {
	BaseNodeData     `yaml:",inline"`
	OutputType       string            `yaml:"output_type"`
	Variables        [][]string        `yaml:"variables"`
	AdvancedSettings *AdvancedSettings `yaml:"advanced_settings,omitempty"`
}

// AdvancedSettings is the optional grouped-aggregation config.
type AdvancedSettings struct {
	GroupEnabled bool              `yaml:"group_enabled"`
	Groups       []AggregatorGroup `yaml:"groups,omitempty"`
}

type AggregatorGroup struct {
	GroupName  string     `yaml:"group_name"`
	OutputType string     `yaml:"output_type"`
	Variables  [][]string `yaml:"variables"`
}

func init() {
	registerNodeType(NodeTypeVariableAggregator, func() NodeData { return &VariableAggregatorNodeData{} })
	// "variable-assigner" is the legacy alias for variable-aggregator in older
	// DSL files; later it was repurposed for the v1 assigner. We register it
	// under the generic VariableAssignerNodeData below which can hold either.
}

// ----------------------------------------------------------------------------
// assigner (v2)  +  variable-assigner (legacy: v1 / aggregator)
// ----------------------------------------------------------------------------

// VariableAssignerNodeData is the payload for the "assigner" (v2) node and
// also serves as a permissive container for the legacy "variable-assigner"
// type, which can mean either:
//   - the v1 single-target assigner (write_mode + assigned_variable_selector),
//     or
//   - the deprecated aggregator alias (output_type + variables[][]).
//
// We keep raw aliases for the legacy fields so callers can branch on Version.
//
// Reference:
//   - graphon/src/graphon/nodes/variable_assigner/v2/entities.py (assigner v2)
//   - graphon/src/graphon/nodes/variable_assigner/v1/node_data.py (legacy v1)
type VariableAssignerNodeData struct {
	BaseNodeData `yaml:",inline"`

	// v2 fields.
	Items []VariableOperationItem `yaml:"items,omitempty"`

	// legacy v1 fields (kept as raw yaml.Node to avoid hard-coding a schema
	// that has shifted across versions).
	AssignedVariableSelector []string `yaml:"assigned_variable_selector,omitempty"`
	WriteMode                string   `yaml:"write_mode,omitempty"`
	InputVariableSelector    []string `yaml:"input_variable_selector,omitempty"`

	// legacy aggregator alias - see comment on VariableAggregatorNodeData.
	OutputType string     `yaml:"output_type,omitempty"`
	Variables  [][]string `yaml:"variables,omitempty"`

	// Catch-all for any unknown fields so older DSLs don't lose data.
	Extra yaml.Node `yaml:"-"`
}

// VariableOperationItem is one entry of the v2 assigner's items[].
type VariableOperationItem struct {
	VariableSelector []string `yaml:"variable_selector"`
	InputType        string   `yaml:"input_type"` // constant | variable
	Operation        string   `yaml:"operation"`  // over-write | clear | append | extend | set | += | -= | *= | /=
	Value            any      `yaml:"value,omitempty"`
}

func init() {
	registerNodeType(NodeTypeAssigner, func() NodeData { return &VariableAssignerNodeData{} })
	// Same struct also handles the legacy "variable-assigner" alias.
	registerNodeType(NodeTypeLegacyAggregator, func() NodeData { return &VariableAssignerNodeData{} })
}

// ----------------------------------------------------------------------------
// list-operator
// ----------------------------------------------------------------------------

// ListOperatorNodeData is the payload for the "list-operator" node.
//
// Reference: graphon/src/graphon/nodes/list_operator/entities.py
type ListOperatorNodeData struct {
	BaseNodeData `yaml:",inline"`
	Variable     []string       `yaml:"variable"`
	FilterBy     FilterBy       `yaml:"filter_by"`
	OrderBy      OrderByConfig  `yaml:"order_by"`
	Limit        Limit          `yaml:"limit"`
	ExtractBy    *ExtractConfig `yaml:"extract_by,omitempty"`
}

type FilterBy struct {
	Enabled    bool              `yaml:"enabled"`
	Conditions []FilterCondition `yaml:"conditions,omitempty"`
}

type FilterCondition struct {
	Key                string `yaml:"key,omitempty"`
	ComparisonOperator string `yaml:"comparison_operator"`
	Value              any    `yaml:"value,omitempty"`
}

type OrderByConfig struct {
	Enabled bool   `yaml:"enabled"`
	Key     string `yaml:"key,omitempty"`
	Value   string `yaml:"value,omitempty"` // asc | desc
}

type Limit struct {
	Enabled bool `yaml:"enabled"`
	Size    int  `yaml:"size,omitempty"`
}

type ExtractConfig struct {
	Enabled bool   `yaml:"enabled"`
	Serial  string `yaml:"serial,omitempty"`
}

func init() {
	registerNodeType(NodeTypeListOperator, func() NodeData { return &ListOperatorNodeData{} })
}
