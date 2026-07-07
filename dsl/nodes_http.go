package dsl

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// ----------------------------------------------------------------------------
// http-request
// ----------------------------------------------------------------------------

// HTTPRequestNodeData is the payload for the "http-request" node.
//
// Heads up: Headers and Params are *strings* in line-format "key:value", not
// maps. Use ParseHTTPLineMap to convert them when needed.
//
// Reference: graphon/src/graphon/nodes/http_request/entities.py
type HTTPRequestNodeData struct {
	BaseNodeData  `yaml:",inline"`
	Method        string                   `yaml:"method"`
	URL           string                   `yaml:"url"`
	Authorization HTTPRequestAuthorization `yaml:"authorization"`
	Headers       string                   `yaml:"headers"`
	Params        string                   `yaml:"params"`
	Body          *HTTPRequestBody         `yaml:"body,omitempty"`
	Timeout       *HTTPRequestTimeout      `yaml:"timeout,omitempty"`
	SSLVerify     *bool                    `yaml:"ssl_verify,omitempty"`
}

type HTTPRequestAuthorization struct {
	Type   string                          `yaml:"type"` // no-auth | api-key
	Config *HTTPRequestAuthorizationConfig `yaml:"config,omitempty"`
}

type HTTPRequestAuthorizationConfig struct {
	Type   string `yaml:"type"` // basic | bearer | custom
	APIKey string `yaml:"api_key"`
	Header string `yaml:"header,omitempty"`
}

// HTTPRequestBody mirrors graphon's BodyData container. Two compatibility
// shapes are accepted for the data field:
//   - DSL >= 0.5 : data is a list of HTTPBodyData entries
//   - DSL <= 0.4 : data is a string (raw text); it is converted to a single
//     text-typed entry, matching graphon's check_data() validator
//
// Reference: graphon/src/graphon/nodes/http_request/entities.py: Body.check_data
type HTTPRequestBody struct {
	Type string         `yaml:"type"` // none | form-data | x-www-form-urlencoded | raw-text | json | binary
	Data []HTTPBodyData `yaml:"data,omitempty"`
}

// UnmarshalYAML accepts both shapes of body.data described above.
func (b *HTTPRequestBody) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("http body: expected mapping, got kind=%d", value.Kind)
	}
	// Walk the mapping manually so we can intercept the "data" field.
	for i := 0; i+1 < len(value.Content); i += 2 {
		k := value.Content[i]
		v := value.Content[i+1]
		switch k.Value {
		case "type":
			if err := v.Decode(&b.Type); err != nil {
				return fmt.Errorf("http body type: %w", err)
			}
		case "data":
			switch v.Kind {
			case yaml.ScalarNode:
				// Legacy: a single string. Empty string => empty list.
				var s string
				if err := v.Decode(&s); err != nil {
					return fmt.Errorf("http body data (scalar): %w", err)
				}
				if s == "" {
					b.Data = nil
				} else {
					b.Data = []HTTPBodyData{{Key: "", Type: "text", Value: s}}
				}
			case yaml.SequenceNode:
				if err := v.Decode(&b.Data); err != nil {
					return fmt.Errorf("http body data (sequence): %w", err)
				}
			case 0:
				// Empty / null
				b.Data = nil
			default:
				return fmt.Errorf("http body data: unsupported yaml kind %d", v.Kind)
			}
		}
	}
	return nil
}

type HTTPBodyData struct {
	Key   string   `yaml:"key,omitempty"`
	Type  string   `yaml:"type"` // file | text
	Value string   `yaml:"value,omitempty"`
	File  []string `yaml:"file,omitempty"` // value_selector when type=file
}

type HTTPRequestTimeout struct {
	Connect *int `yaml:"connect,omitempty"`
	Read    *int `yaml:"read,omitempty"`
	Write   *int `yaml:"write,omitempty"`
}

func init() {
	registerNodeType(NodeTypeHTTPRequest, func() NodeData { return &HTTPRequestNodeData{} })
}

// ----------------------------------------------------------------------------
// tool
// ----------------------------------------------------------------------------

// ToolNodeData is the payload for the "tool" node.
//
// Reference: graphon/src/graphon/nodes/tool/entities.py
type ToolNodeData struct {
	BaseNodeData           `yaml:",inline"`
	ProviderID             string               `yaml:"provider_id"`
	ProviderType           string               `yaml:"provider_type"`
	ProviderName           string               `yaml:"provider_name,omitempty"`
	ToolName               string               `yaml:"tool_name"`
	ToolLabel              string               `yaml:"tool_label,omitempty"`
	ToolConfigurations     map[string]any       `yaml:"tool_configurations,omitempty"`
	CredentialID           string               `yaml:"credential_id,omitempty"`
	PluginUniqueIdentifier string               `yaml:"plugin_unique_identifier,omitempty"`
	ToolParameters         map[string]ToolInput `yaml:"tool_parameters,omitempty"`
	ToolNodeVersion        string               `yaml:"tool_node_version,omitempty"`
}

// ToolInput is one entry of ToolNodeData.ToolParameters.
//
// The Type field disambiguates how Value should be interpreted:
//   - "constant" : Value is a literal of any scalar/dict/list type.
//   - "variable" : Value is a list[string] selector into the variable pool.
//   - "mixed"    : Value is a string template that may contain {{#node.var#}}.
//
// Backward-compatibility: in DSL <= 0.4 (and certain later "tool_node_version=null"
// flows), tool_parameters values were stored as bare scalars/strings rather
// than the {type, value} struct. We accept that form too and synthesise a
// "mixed" wrapper to match the legacy parser inside Dify.
type ToolInput struct {
	Type  string `yaml:"type"`
	Value any    `yaml:"value,omitempty"`
}

// UnmarshalYAML accepts both the new struct form and the legacy scalar form.
func (t *ToolInput) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.MappingNode:
		// New form: {type: ..., value: ...}. Use a private alias to avoid
		// recursing into UnmarshalYAML.
		type alias ToolInput
		var a alias
		if err := value.Decode(&a); err != nil {
			return err
		}
		*t = ToolInput(a)
		// Some legacy files persist a bare {value: x} without an explicit type;
		// treat that as the mixed/template form.
		if t.Type == "" {
			t.Type = "mixed"
		}
		return nil
	case yaml.ScalarNode, yaml.SequenceNode:
		// Legacy form: bare scalar/sequence. Wrap into a mixed-type input.
		var raw any
		if err := value.Decode(&raw); err != nil {
			return err
		}
		t.Type = "mixed"
		t.Value = raw
		return nil
	case 0:
		// Empty / null entry; leave the zero value.
		return nil
	default:
		// Unknown YAML kind: fall back to raw decode of value to preserve data.
		var raw any
		if err := value.Decode(&raw); err != nil {
			return err
		}
		t.Type = "mixed"
		t.Value = raw
		return nil
	}
}

// ToolProviderType constants.
const (
	ToolProviderPlugin           = "plugin"
	ToolProviderBuiltin          = "builtin"
	ToolProviderWorkflow         = "workflow"
	ToolProviderAPI              = "api"
	ToolProviderApp              = "app"
	ToolProviderDatasetRetrieval = "dataset-retrieval"
	ToolProviderMCP              = "mcp"
)

func init() {
	registerNodeType(NodeTypeTool, func() NodeData { return &ToolNodeData{} })
}
