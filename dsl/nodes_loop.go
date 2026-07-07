package dsl

// ----------------------------------------------------------------------------
// iteration  +  iteration-start
// ----------------------------------------------------------------------------

// IterationNodeData is the payload for the "iteration" container node.
//
// Reference: graphon/src/graphon/nodes/iteration/entities.py
type IterationNodeData struct {
	BaseNodeData     `yaml:",inline"`
	StartNodeID      string   `yaml:"start_node_id,omitempty"`
	IteratorSelector []string `yaml:"iterator_selector"`
	OutputSelector   []string `yaml:"output_selector"`
	OutputType       string   `yaml:"output_type,omitempty"` // historical alias for output type
	IsParallel       bool     `yaml:"is_parallel,omitempty"`
	ParallelNums     int      `yaml:"parallel_nums,omitempty"`
	ErrorHandleMode  string   `yaml:"error_handle_mode,omitempty"` // terminated | continue-on-error | remove-abnormal-output
	FlattenOutput    bool     `yaml:"flatten_output,omitempty"`
	ParentLoopID     string   `yaml:"parent_loop_id,omitempty"`
}

// IterationStartNodeData is the empty subgraph entry for an iteration.
type IterationStartNodeData struct {
	BaseNodeData `yaml:",inline"`
}

func init() {
	registerNodeType(NodeTypeIteration, func() NodeData { return &IterationNodeData{} })
	registerNodeType(NodeTypeIterationStart, func() NodeData { return &IterationStartNodeData{} })
}

// ----------------------------------------------------------------------------
// loop  +  loop-start  +  loop-end
// ----------------------------------------------------------------------------

// LoopNodeData is the payload for the "loop" container node.
//
// Reference: graphon/src/graphon/nodes/loop/entities.py
type LoopNodeData struct {
	BaseNodeData    `yaml:",inline"`
	StartNodeID     string             `yaml:"start_node_id,omitempty"`
	LoopCount       int                `yaml:"loop_count"`
	BreakConditions []Condition        `yaml:"break_conditions"`
	LogicalOperator string             `yaml:"logical_operator,omitempty"`
	LoopVariables   []LoopVariableData `yaml:"loop_variables,omitempty"`
	Outputs         map[string]any     `yaml:"outputs,omitempty"`
}

type LoopVariableData struct {
	Label     string `yaml:"label"`
	VarType   string `yaml:"var_type"`
	ValueType string `yaml:"value_type"` // variable | constant
	Value     any    `yaml:"value,omitempty"`
}

type LoopStartNodeData struct {
	BaseNodeData `yaml:",inline"`
}
type LoopEndNodeData struct {
	BaseNodeData `yaml:",inline"`
}

func init() {
	registerNodeType(NodeTypeLoop, func() NodeData { return &LoopNodeData{} })
	registerNodeType(NodeTypeLoopStart, func() NodeData { return &LoopStartNodeData{} })
	registerNodeType(NodeTypeLoopEnd, func() NodeData { return &LoopEndNodeData{} })
}
