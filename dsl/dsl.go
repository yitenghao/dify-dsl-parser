// package dify provides a parser for Dify workflow DSL (YAML).
//
// The parser uses a two-phase decode:
//
//  1. The YAML is decoded into a tree of generic Node configurations,
//     where each node's "data" field is preserved as a *yaml.Node.
//
//  2. Each node is then re-decoded into a concrete NodeData struct
//     selected from a registry keyed by data.type.
//
// The parser is permissive: unknown node types are kept as
// UnknownNodeData so older or extended DSL files still parse.
package dsl

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// DSL is the root document of a Dify app export.
//
// Reference: api/services/app_dsl_service.py:export_dsl
type DSL struct {
	// Version is the DSL schema version (e.g. "0.3.1", "0.6.0").
	Version string `yaml:"version"`
	// Kind is always "app".
	Kind string `yaml:"kind"`
	// App contains app metadata (name, mode, icon...).
	App AppMeta `yaml:"app"`
	// Workflow is set when App.Mode is "workflow" or "advanced-chat".
	Workflow *Workflow `yaml:"workflow,omitempty"`
	// ModelConfig is set for the legacy "chat" / "agent-chat" / "completion" modes.
	// We keep it as raw YAML to avoid hard-coding the legacy schema.
	ModelConfig yaml.Node `yaml:"model_config,omitempty"`
	// Dependencies lists plugin dependencies (providers, tools).
	Dependencies []Dependency `yaml:"dependencies,omitempty"`
}

// AppMeta is the app block at the root of the DSL.
type AppMeta struct {
	Name                string `yaml:"name"`
	Mode                string `yaml:"mode"` // workflow | advanced-chat | chat | agent-chat | completion
	Icon                string `yaml:"icon,omitempty"`
	IconType            string `yaml:"icon_type,omitempty"`
	IconBackground      string `yaml:"icon_background,omitempty"`
	Description         string `yaml:"description,omitempty"`
	UseIconAsAnswerIcon bool   `yaml:"use_icon_as_answer_icon,omitempty"`
}

// AppMode constants.
const (
	AppModeWorkflow     = "workflow"
	AppModeAdvancedChat = "advanced-chat"
	AppModeChat         = "chat"
	AppModeAgentChat    = "agent-chat"
	AppModeCompletion   = "completion"
)

// Dependency is a plugin dependency entry.
type Dependency struct {
	Type    string    `yaml:"type"`
	Value   yaml.Node `yaml:"value,omitempty"`
	Current yaml.Node `yaml:"current_identifier,omitempty"`
}

// Parse reads and decodes a DSL YAML document from r.
func Parse(r io.Reader) (*DSL, error) {
	dec := yaml.NewDecoder(r)
	var d DSL
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("decode dsl: %w", err)
	}
	if err := d.normalize(); err != nil {
		return nil, err
	}
	return &d, nil
}

// ParseFile reads and decodes a DSL YAML document from a file path.
func ParseFile(path string) (*DSL, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	d, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return d, nil
}

// normalize fills in defaults and rejects clearly invalid documents.
//
// This mirrors the behaviour of AppDslService.import_app:
//   - missing version becomes "0.1.0"
//   - missing kind becomes "app"
func (d *DSL) normalize() error {
	if d.Version == "" {
		d.Version = "0.1.0"
	}
	if d.Kind == "" {
		d.Kind = "app"
	}
	if d.Kind != "app" {
		return fmt.Errorf("dsl: unsupported kind %q (only \"app\" is allowed)", d.Kind)
	}
	if d.App.Mode == "" {
		return fmt.Errorf("dsl: app.mode is required")
	}
	switch d.App.Mode {
	case AppModeWorkflow, AppModeAdvancedChat:
		if d.Workflow == nil {
			return fmt.Errorf("dsl: app.mode=%s requires the workflow block", d.App.Mode)
		}
	case AppModeChat, AppModeAgentChat, AppModeCompletion:
		// model_config-based legacy modes; nothing to validate here.
	default:
		// Permissive: unknown modes still parse, caller can decide.
	}
	return nil
}

// IsWorkflow reports whether the DSL describes a workflow-graph app.
func (d *DSL) IsWorkflow() bool {
	return d.App.Mode == AppModeWorkflow || d.App.Mode == AppModeAdvancedChat
}
