package dsl

import "gopkg.in/yaml.v3"

// Workflow is the workflow block of a DSL document.
//
// Reference: api/models/workflow.py: WorkflowContentDict
type Workflow struct {
	Graph                 Graph            `yaml:"graph"`
	Features              *yaml.Node       `yaml:"features,omitempty"`
	EnvironmentVariables  []ScopedVariable `yaml:"environment_variables,omitempty"`
	ConversationVariables []ScopedVariable `yaml:"conversation_variables,omitempty"`
	RagPipelineVariables  []map[string]any `yaml:"rag_pipeline_variables,omitempty"`
}

// Graph holds the nodes and edges of a workflow.
type Graph struct {
	Nodes    []Node    `yaml:"nodes"`
	Edges    []Edge    `yaml:"edges"`
	Viewport yaml.Node `yaml:"viewport,omitempty"`
}

// Edge is a connection between two nodes.
//
// The sourceHandle field carries branching semantics:
//   - "source"             : default single output
//   - "true"/"false"/<id>  : if-else case routing
//   - <class_id>           : question-classifier routing
//   - <action_id>          : human-input button routing
//   - "success-branch"
//     /"fail-branch"       : error-strategy=fail-branch routing
//
// Reference: graphon/src/graphon/graph/edge.py
type Edge struct {
	ID           string    `yaml:"id"`
	Source       string    `yaml:"source"`
	Target       string    `yaml:"target"`
	SourceHandle string    `yaml:"sourceHandle,omitempty"`
	TargetHandle string    `yaml:"targetHandle,omitempty"`
	Type         string    `yaml:"type,omitempty"`
	Data         yaml.Node `yaml:"data,omitempty"`
}

// EffectiveSourceHandle returns the source handle, defaulting to "source"
// when the field is absent (which matches graphon's runtime default).
func (e Edge) EffectiveSourceHandle() string {
	if e.SourceHandle == "" {
		return "source"
	}
	return e.SourceHandle
}

// ScopedVariable is an environment_variable / conversation_variable entry.
//
// Reference: api/factories/variable_factory.py:_build_variable_from_mapping
type ScopedVariable struct {
	ID          string `yaml:"id,omitempty"`
	Name        string `yaml:"name"`
	ValueType   string `yaml:"value_type"`
	Value       any    `yaml:"value"`
	Description string `yaml:"description,omitempty"`
}
