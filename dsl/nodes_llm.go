package dsl

import "gopkg.in/yaml.v3"

// ----------------------------------------------------------------------------
// shared types
// ----------------------------------------------------------------------------

// ModelConfig is shared by llm / question-classifier / parameter-extractor.
//
// Reference: graphon/src/graphon/nodes/llm/entities.py: ModelConfig
type ModelConfig struct {
	Provider         string         `yaml:"provider"`
	Name             string         `yaml:"name"`
	Mode             string         `yaml:"mode,omitempty"` // chat | completion
	CompletionParams map[string]any `yaml:"completion_params,omitempty"`
}

// ContextConfig is the optional retrieval context fed into an LLM.
type ContextConfig struct {
	Enabled          bool     `yaml:"enabled"`
	VariableSelector []string `yaml:"variable_selector,omitempty"`
}

// VisionConfig is the optional image-input config.
type VisionConfig struct {
	Enabled bool                 `yaml:"enabled,omitempty"`
	Configs *VisionConfigOptions `yaml:"configs,omitempty"`
}

// VisionConfigOptions is VisionConfig.Configs.
type VisionConfigOptions struct {
	VariableSelector []string `yaml:"variable_selector,omitempty"`
	Detail           string   `yaml:"detail,omitempty"` // high | low | auto
}

// MemoryConfig is the chat-history controller.
type MemoryConfig struct {
	RolePrefix          *MemoryRolePrefix  `yaml:"role_prefix,omitempty"`
	Window              MemoryWindowConfig `yaml:"window"`
	QueryPromptTemplate string             `yaml:"query_prompt_template,omitempty"`
}

type MemoryRolePrefix struct {
	User      string `yaml:"user"`
	Assistant string `yaml:"assistant"`
}

type MemoryWindowConfig struct {
	Enabled bool `yaml:"enabled"`
	Size    *int `yaml:"size,omitempty"`
}

// PromptConfig holds optional jinja2 variable bindings.
type PromptConfig struct {
	Jinja2Variables []VariableSelector `yaml:"jinja2_variables,omitempty"`
}

// ----------------------------------------------------------------------------
// llm
// ----------------------------------------------------------------------------

// LLMNodeData is the payload for the "llm" node.
//
// PromptTemplate accepts two shapes:
//   - a list of chat messages (chat mode)
//   - a single completion-template object (completion mode)
//
// Both shapes are kept as a raw yaml.Node so callers can decode whichever
// form they expect; PromptMessages() and PromptCompletion() are convenience
// helpers that decode lazily.
//
// Reference: graphon/src/graphon/nodes/llm/entities.py: LLMNodeData
type LLMNodeData struct {
	BaseNodeData            `yaml:",inline"`
	Model                   ModelConfig   `yaml:"model"`
	PromptTemplate          yaml.Node     `yaml:"prompt_template"`
	PromptConfig            *PromptConfig `yaml:"prompt_config,omitempty"`
	Memory                  *MemoryConfig `yaml:"memory,omitempty"`
	Context                 ContextConfig `yaml:"context"`
	Vision                  VisionConfig  `yaml:"vision,omitempty"`
	StructuredOutput        yaml.Node     `yaml:"structured_output,omitempty"`
	StructuredOutputEnabled bool          `yaml:"structured_output_enabled,omitempty"`
	StructuredOutputSwitch  bool          `yaml:"structured_output_switch_on,omitempty"`
	ReasoningFormat         string        `yaml:"reasoning_format,omitempty"`
}

// ChatModelMessage is one entry of an LLM chat-mode prompt template.
type ChatModelMessage struct {
	Role        string `yaml:"role"` // system | user | assistant
	Text        string `yaml:"text"`
	EditionType string `yaml:"edition_type,omitempty"` // basic | jinja2
	Jinja2Text  string `yaml:"jinja2_text,omitempty"`
}

// CompletionModelPromptTemplate is the completion-mode prompt body.
type CompletionModelPromptTemplate struct {
	Text        string `yaml:"text"`
	EditionType string `yaml:"edition_type,omitempty"`
	Jinja2Text  string `yaml:"jinja2_text,omitempty"`
}

// PromptMessages decodes prompt_template as a chat-message list.
// Returns (nil, false) if the prompt_template is not a sequence.
func (d *LLMNodeData) PromptMessages() ([]ChatModelMessage, bool) {
	if d.PromptTemplate.Kind != yaml.SequenceNode {
		return nil, false
	}
	var out []ChatModelMessage
	if err := d.PromptTemplate.Decode(&out); err != nil {
		return nil, false
	}
	return out, true
}

// PromptCompletion decodes prompt_template as a completion-mode template.
// Returns (nil, false) if the prompt_template is not a mapping.
func (d *LLMNodeData) PromptCompletion() (*CompletionModelPromptTemplate, bool) {
	if d.PromptTemplate.Kind != yaml.MappingNode {
		return nil, false
	}
	var out CompletionModelPromptTemplate
	if err := d.PromptTemplate.Decode(&out); err != nil {
		return nil, false
	}
	return &out, true
}

func init() {
	registerNodeType(NodeTypeLLM, func() NodeData { return &LLMNodeData{} })
}

// ----------------------------------------------------------------------------
// question-classifier
// ----------------------------------------------------------------------------

// QuestionClassifierNodeData is the payload for the "question-classifier" node.
// Each entry of Classes corresponds to a separate sourceHandle on outgoing edges.
//
// Reference: graphon/src/graphon/nodes/question_classifier/entities.py
type QuestionClassifierNodeData struct {
	BaseNodeData          `yaml:",inline"`
	QueryVariableSelector []string      `yaml:"query_variable_selector"`
	Model                 ModelConfig   `yaml:"model"`
	Classes               []ClassConfig `yaml:"classes"`
	Instruction           string        `yaml:"instruction,omitempty"`
	Memory                *MemoryConfig `yaml:"memory,omitempty"`
	Vision                VisionConfig  `yaml:"vision,omitempty"`
}

type ClassConfig struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

func init() {
	registerNodeType(NodeTypeQuestionClassifier, func() NodeData { return &QuestionClassifierNodeData{} })
}

// ----------------------------------------------------------------------------
// parameter-extractor
// ----------------------------------------------------------------------------

// ParameterExtractorNodeData is the payload for the "parameter-extractor" node.
//
// Reference: graphon/src/graphon/nodes/parameter_extractor/entities.py
type ParameterExtractorNodeData struct {
	BaseNodeData  `yaml:",inline"`
	Model         ModelConfig       `yaml:"model"`
	Query         []string          `yaml:"query"`
	Parameters    []ParameterConfig `yaml:"parameters"`
	Instruction   string            `yaml:"instruction,omitempty"`
	Memory        *MemoryConfig     `yaml:"memory,omitempty"`
	ReasoningMode string            `yaml:"reasoning_mode,omitempty"` // function_call | prompt
	Vision        VisionConfig      `yaml:"vision,omitempty"`
}

// ParameterConfig is one entry of ParameterExtractorNodeData.Parameters.
type ParameterConfig struct {
	Name        string   `yaml:"name"`
	Type        string   `yaml:"type"` // string | number | boolean | array[*] | bool (legacy) | select (legacy)
	Options     []string `yaml:"options,omitempty"`
	Description string   `yaml:"description,omitempty"`
	Required    bool     `yaml:"required,omitempty"`
}

func init() {
	registerNodeType(NodeTypeParameterExtractor, func() NodeData { return &ParameterExtractorNodeData{} })
}

// ----------------------------------------------------------------------------
// knowledge-retrieval
// ----------------------------------------------------------------------------

// KnowledgeRetrievalNodeData is the payload for the "knowledge-retrieval" node.
//
// Reference: api/core/workflow/nodes/knowledge_retrieval/entities.py
type KnowledgeRetrievalNodeData struct {
	BaseNodeData                `yaml:",inline"`
	QueryVariableSelector       any          `yaml:"query_variable_selector,omitempty"` // []string or string
	QueryAttachmentSelector     any          `yaml:"query_attachment_selector,omitempty"`
	DatasetIDs                  []string     `yaml:"dataset_ids"`
	RetrievalMode               string       `yaml:"retrieval_mode"` // single | multiple
	MultipleRetrievalConfig     yaml.Node    `yaml:"multiple_retrieval_config,omitempty"`
	SingleRetrievalConfig       yaml.Node    `yaml:"single_retrieval_config,omitempty"`
	MetadataFilteringMode       string       `yaml:"metadata_filtering_mode,omitempty"`
	MetadataModelConfig         yaml.Node    `yaml:"metadata_model_config,omitempty"`
	MetadataFilteringConditions yaml.Node    `yaml:"metadata_filtering_conditions,omitempty"`
	Vision                      VisionConfig `yaml:"vision,omitempty"`
}

func init() {
	registerNodeType(NodeTypeKnowledgeRetrieval, func() NodeData { return &KnowledgeRetrievalNodeData{} })
}
