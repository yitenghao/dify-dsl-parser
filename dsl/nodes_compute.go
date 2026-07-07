package dsl

import "gopkg.in/yaml.v3"

// VariableSelector is a binding from a node-local variable name to a
// value pulled from the variable pool.
//
// Reference: graphon/src/graphon/nodes/base/entities.py: VariableSelector
type VariableSelector struct {
	Variable      string   `yaml:"variable"`
	ValueSelector []string `yaml:"value_selector"`
}

// ----------------------------------------------------------------------------
// code
// ----------------------------------------------------------------------------

// CodeNodeData is the payload for the "code" node.
//
// Reference: graphon/src/graphon/nodes/code/entities.py
type CodeNodeData struct {
	BaseNodeData `yaml:",inline"`
	Variables    []VariableSelector    `yaml:"variables"`
	CodeLanguage string                `yaml:"code_language"` // python3 | javascript
	Code         string                `yaml:"code"`
	Outputs      map[string]CodeOutput `yaml:"outputs"`
	Dependencies []CodeDependency      `yaml:"dependencies,omitempty"`
}

// CodeOutput declares a single output field of a code node.
type CodeOutput struct {
	Type     string                `yaml:"type"`
	Children map[string]CodeOutput `yaml:"children,omitempty"`
}

// CodeDependency is one pip / npm dependency.
type CodeDependency struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// CodeLanguage values.
const (
	CodeLangPython3    = "python3"
	CodeLangJavaScript = "javascript"
	CodeLangJinja2     = "jinja2"
)

func init() {
	registerNodeType(NodeTypeCode, func() NodeData { return &CodeNodeData{} })
}

// ----------------------------------------------------------------------------
// template-transform
// ----------------------------------------------------------------------------

// TemplateTransformNodeData is the payload for the "template-transform" node.
//
// Reference: graphon/src/graphon/nodes/template_transform/entities.py
type TemplateTransformNodeData struct {
	BaseNodeData `yaml:",inline"`
	Variables    []VariableSelector `yaml:"variables"`
	Template     string             `yaml:"template"`
}

func init() {
	registerNodeType(NodeTypeTemplateTransform, func() NodeData { return &TemplateTransformNodeData{} })
}

// ----------------------------------------------------------------------------
// document-extractor
// ----------------------------------------------------------------------------

// DocumentExtractorNodeData is the payload for the "document-extractor" node.
type DocumentExtractorNodeData struct {
	BaseNodeData     `yaml:",inline"`
	VariableSelector []string `yaml:"variable_selector"`
}

func init() {
	registerNodeType(NodeTypeDocumentExtractor, func() NodeData { return &DocumentExtractorNodeData{} })
}

// ensure yaml.Node import is referenced even if all uses are conditional
var _ = yaml.Node{}
