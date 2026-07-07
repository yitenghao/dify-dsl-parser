package dsl

// ----------------------------------------------------------------------------
// if-else
// ----------------------------------------------------------------------------

// IfElseNodeData is the payload for the "if-else" branch node.
//
// The schema evolved across DSL versions:
//   - Legacy (DSL <= 0.4): single conditions[] + logical_operator
//   - Current: cases[] (each case_id becomes a sourceHandle on outgoing edges)
//
// Both shapes are accepted; iter_cases below normalizes them.
//
// Reference: graphon/src/graphon/nodes/if_else/entities.py
type IfElseNodeData struct {
	BaseNodeData `yaml:",inline"`

	// New schema.
	Cases []IfElseCase `yaml:"cases,omitempty"`

	// Legacy schema. logical_operator + conditions on the data block itself.
	LogicalOperator string      `yaml:"logical_operator,omitempty"` // "and" | "or"
	Conditions      []Condition `yaml:"conditions,omitempty"`
}

// IfElseCase is one branch of an if-else node.
//
// case_id values become the sourceHandle on outgoing edges, with the
// implicit "false" handle reserved for the catch-all else branch.
type IfElseCase struct {
	CaseID          string      `yaml:"case_id" yaml:",omitempty"`
	LogicalOperator string      `yaml:"logical_operator"`
	Conditions      []Condition `yaml:"conditions"`
}

// Condition is one row of an if-else / list-operator filter.
//
// Reference: graphon/src/graphon/utils/condition/entities.py
type Condition struct {
	VariableSelector     []string              `yaml:"variable_selector"`
	ComparisonOperator   string                `yaml:"comparison_operator"`
	Value                any                   `yaml:"value,omitempty"` // string | []string | bool | nil
	SubVariableCondition *SubVariableCondition `yaml:"sub_variable_condition,omitempty"`
}

// SubVariableCondition is the optional inner-condition block used to filter
// over array[object] elements.
type SubVariableCondition struct {
	LogicalOperator string         `yaml:"logical_operator"`
	Conditions      []SubCondition `yaml:"conditions"`
}

// SubCondition is one inner row of a SubVariableCondition.
type SubCondition struct {
	Key                string `yaml:"key"`
	ComparisonOperator string `yaml:"comparison_operator"`
	Value              any    `yaml:"value,omitempty"`
}

// IterCases returns the effective case list, normalizing legacy conditions[]
// into a single synthetic case with case_id="true".
//
// This mirrors graphon's IfElseNodeData.iter_cases().
func (d *IfElseNodeData) IterCases() []IfElseCase {
	if len(d.Cases) > 0 {
		return d.Cases
	}
	if len(d.Conditions) == 0 {
		return nil
	}
	op := d.LogicalOperator
	if op == "" {
		op = "and"
	}
	return []IfElseCase{{
		CaseID:          "true",
		LogicalOperator: op,
		Conditions:      d.Conditions,
	}}
}

func init() {
	registerNodeType(NodeTypeIfElse, func() NodeData { return &IfElseNodeData{} })
}
