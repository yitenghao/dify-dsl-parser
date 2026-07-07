package dsl

import "gopkg.in/yaml.v3"

// ----------------------------------------------------------------------------
// agent
// ----------------------------------------------------------------------------

// AgentNodeData is the payload for the "agent" node.
//
// Reference: api/core/workflow/nodes/agent/entities.py
type AgentNodeData struct {
	BaseNodeData              `yaml:",inline"`
	AgentStrategyProviderName string                `yaml:"agent_strategy_provider_name"`
	AgentStrategyName         string                `yaml:"agent_strategy_name"`
	AgentStrategyLabel        string                `yaml:"agent_strategy_label,omitempty"`
	Memory                    *MemoryConfig         `yaml:"memory,omitempty"`
	ToolNodeVersion           string                `yaml:"tool_node_version,omitempty"`
	AgentParameters           map[string]AgentInput `yaml:"agent_parameters,omitempty"`
}

// AgentInput is one entry of AgentNodeData.AgentParameters; its shape is
// dynamic (depends on the strategy), so Value is any.
type AgentInput struct {
	Type  string `yaml:"type"` // mixed | variable | constant
	Value any    `yaml:"value,omitempty"`
}

func init() {
	registerNodeType(NodeTypeAgent, func() NodeData { return &AgentNodeData{} })
}

// ----------------------------------------------------------------------------
// human-input
// ----------------------------------------------------------------------------

// HumanInputNodeData is the payload for the "human-input" node.
//
// Reference: graphon/src/graphon/nodes/human_input/entities.py
type HumanInputNodeData struct {
	BaseNodeData `yaml:",inline"`
	FormContent  string       `yaml:"form_content,omitempty"`
	Inputs       []FormInput  `yaml:"inputs,omitempty"`
	UserActions  []UserAction `yaml:"user_actions,omitempty"`
	Timeout      int          `yaml:"timeout,omitempty"`
	TimeoutUnit  string       `yaml:"timeout_unit,omitempty"` // hour | day
}

type FormInput struct {
	Type               string            `yaml:"type"` // text-input | paragraph
	OutputVariableName string            `yaml:"output_variable_name"`
	Default            *FormInputDefault `yaml:"default,omitempty"`
}

type FormInputDefault struct {
	Type     string   `yaml:"type"` // variable | constant
	Selector []string `yaml:"selector,omitempty"`
	Value    string   `yaml:"value,omitempty"`
}

type UserAction struct {
	ID          string `yaml:"id"`
	Title       string `yaml:"title"`
	ButtonStyle string `yaml:"button_style,omitempty"` // primary | default | accent | ghost
}

func init() {
	registerNodeType(NodeTypeHumanInput, func() NodeData { return &HumanInputNodeData{} })
}

// ----------------------------------------------------------------------------
// trigger-* and datasource (rag-pipeline only)
// ----------------------------------------------------------------------------

// TriggerNodeData is a permissive payload for the rag-pipeline trigger node
// types ("trigger-webhook", "trigger-schedule", "trigger-plugin"). The exact
// shape of the config block is not stable, so we keep it as raw yaml.
type TriggerNodeData struct {
	BaseNodeData    `yaml:",inline"`
	Config          yaml.Node `yaml:"config,omitempty"`
	WebhookURL      string    `yaml:"webhook_url,omitempty"`
	WebhookDebugURL string    `yaml:"webhook_debug_url,omitempty"`
	SubscriptionID  string    `yaml:"subscription_id,omitempty"`
}

// DatasourceNodeData is the payload for the "datasource" root node used by
// rag-pipeline apps in lieu of a "start" node.
type DatasourceNodeData struct {
	BaseNodeData `yaml:",inline"`
	Config       yaml.Node        `yaml:"config,omitempty"`
	Variables    []VariableEntity `yaml:"variables,omitempty"`
}

func init() {
	registerNodeType(NodeTypeTriggerWebhook, func() NodeData { return &TriggerNodeData{} })
	registerNodeType(NodeTypeTriggerSchedule, func() NodeData { return &TriggerNodeData{} })
	registerNodeType(NodeTypeTriggerPlugin, func() NodeData { return &TriggerNodeData{} })
	registerNodeType(NodeTypeDatasource, func() NodeData { return &DatasourceNodeData{} })
}
