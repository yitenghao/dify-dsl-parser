// Package server exposes the dsl + engine packages over an HTTP API so a
// browser-based flow editor (React Flow) can import/export Dify DSL, save and
// publish versioned drafts, and run interactive tests with per-node execution
// tracing.
//
// The wire format on every editor-facing endpoint is the Dify DSL itself,
// expressed as JSON (the exact same shape as the exported YAML, just JSON
// instead of YAML). The frontend therefore never invents its own graph model:
// it edits the DSL directly, and this server is the single source of truth for
// parsing, validation and canonical (re-)serialization.
package server

import (
	"bytes"
	"encoding/json"
	"fmt"

	"dify-dsl-parser/dsl"

	"gopkg.in/yaml.v3"
)

// jsonToYAML converts a JSON document (as sent by the editor) into canonical
// YAML bytes. JSON objects decode to map[string]any and JSON numbers to
// float64; yaml.v3 renders those back to the integer/float forms Dify expects.
func jsonToYAML(body []byte) ([]byte, error) {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // keep 244 as 244, not 2.44e2, so YAML stays clean
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	doc = normalizeNumbers(doc)
	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("encode YAML: %w", err)
	}
	return out, nil
}

// yamlToJSONMap converts stored YAML into a JSON-safe map for the editor.
func yamlToJSONMap(yml []byte) (map[string]any, error) {
	var m map[string]any
	if err := yaml.Unmarshal(yml, &m); err != nil {
		return nil, fmt.Errorf("decode YAML: %w", err)
	}
	return m, nil
}

// normalizeNumbers walks a decoded JSON value and turns json.Number into int64
// where possible (else float64) so YAML output uses 10 rather than "10".
func normalizeNumbers(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			t[k] = normalizeNumbers(val)
		}
		return t
	case []any:
		for i, val := range t {
			t[i] = normalizeNumbers(val)
		}
		return t
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		if f, err := t.Float64(); err == nil {
			return f
		}
		return t.String()
	default:
		return v
	}
}

// issueDTO is the JSON form of a dsl.Issue returned to the editor.
type issueDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	NodeID  string `json:"nodeId,omitempty"`
}

// validateYAML parses + validates YAML and returns any issues. A hard parse
// error is returned separately (the document could not even be read); soft
// validation issues come back in the slice.
func validateYAML(yml []byte) (issues []issueDTO, parseErr error) {
	d, err := dsl.Parse(bytes.NewReader(yml))
	if err != nil {
		return nil, err
	}
	for _, is := range d.Validate() {
		issues = append(issues, issueDTO{
			Code:    string(is.Code),
			Message: is.Message,
			NodeID:  is.NodeID,
		})
	}
	return issues, nil
}
