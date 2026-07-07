package dsl

import "gopkg.in/yaml.v3"

// ----------------------------------------------------------------------------
// start
// ----------------------------------------------------------------------------

// StartNodeData is the payload for the "start" node.
//
// Reference: graphon/src/graphon/nodes/start/entities.py
type StartNodeData struct {
	BaseNodeData `yaml:",inline"`
	Variables    []VariableEntity `yaml:"variables,omitempty"`
}

// VariableEntity is a single user-input form field declared by the start node.
//
// Reference: graphon/src/graphon/variables/input_entities.py
type VariableEntity struct {
	Variable                 string    `yaml:"variable"`
	Label                    string    `yaml:"label,omitempty"`
	Description              string    `yaml:"description,omitempty"`
	Type                     string    `yaml:"type"` // VariableEntityType
	Required                 bool      `yaml:"required,omitempty"`
	Hide                     bool      `yaml:"hide,omitempty"`
	Default                  any       `yaml:"default,omitempty"`
	MaxLength                *int      `yaml:"max_length,omitempty"`
	Options                  []string  `yaml:"options,omitempty"`
	AllowedFileTypes         []string  `yaml:"allowed_file_types,omitempty"`
	AllowedFileExtensions    []string  `yaml:"allowed_file_extensions,omitempty"`
	AllowedFileUploadMethods []string  `yaml:"allowed_file_upload_methods,omitempty"`
	JSONSchema               yaml.Node `yaml:"json_schema,omitempty"`
}

// VariableEntityType constants.
const (
	VarTypeTextInput    = "text-input"
	VarTypeSelect       = "select"
	VarTypeParagraph    = "paragraph"
	VarTypeNumber       = "number"
	VarTypeExternalTool = "external_data_tool"
	VarTypeFile         = "file"
	VarTypeFileList     = "file-list"
	VarTypeCheckbox     = "checkbox"
	VarTypeJSONObject   = "json_object"
)

func init() {
	registerNodeType(NodeTypeStart, func() NodeData { return &StartNodeData{} })
}

// ----------------------------------------------------------------------------
// end
// ----------------------------------------------------------------------------

// EndNodeData is the payload for the "end" node (workflow mode terminal).
//
// Reference: graphon/src/graphon/nodes/end/entities.py
type EndNodeData struct {
	BaseNodeData `yaml:",inline"`
	Outputs      []OutputVariable `yaml:"outputs,omitempty"`
}

// OutputVariable maps an output name to a variable selector pulled from any
// node in the graph.
type OutputVariable struct {
	Variable      string   `yaml:"variable"`
	ValueType     string   `yaml:"value_type,omitempty"` // OutputVariableType
	ValueSelector []string `yaml:"value_selector"`
}

func init() {
	registerNodeType(NodeTypeEnd, func() NodeData { return &EndNodeData{} })
}

// ----------------------------------------------------------------------------
// answer
// ----------------------------------------------------------------------------

// AnswerNodeData is the payload for the "answer" node (chatflow streaming
// terminal).
//
// Reference: graphon/src/graphon/nodes/answer/entities.py
type AnswerNodeData struct {
	BaseNodeData `yaml:",inline"`
	// Answer is a template string that may contain {{#node.var#}} references.
	Answer string `yaml:"answer"`
}

func init() {
	registerNodeType(NodeTypeAnswer, func() NodeData { return &AnswerNodeData{} })
}
