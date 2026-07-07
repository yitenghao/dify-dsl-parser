package dsl

import (
	"regexp"
	"strings"
)

// Reserved namespace prefixes used in variable selectors.
//
// Reference: api/core/workflow/variable_prefixes.py
const (
	NSSystem       = "sys"
	NSEnvironment  = "env"
	NSConversation = "conversation"
	NSRagPipeline  = "rag"
)

// ReservedNamespaces is the set of namespace prefixes that don't have to
// resolve to a node ID in graph.nodes.
var ReservedNamespaces = map[string]struct{}{
	NSSystem:       {},
	NSEnvironment:  {},
	NSConversation: {},
	NSRagPipeline:  {},
}

// MinSelectorLength is the minimum length of a value_selector
// (mirrors graphon.variables.consts.SELECTORS_LENGTH).
const MinSelectorLength = 2

// templateVarRegex matches {{#node_id.var.path#}} references.
//
// Reference: graphon/src/graphon/nodes/base/variable_template_parser.py
var templateVarRegex = regexp.MustCompile(
	`\{\{(#[a-zA-Z0-9_]{1,50}(?:\.[a-zA-Z_][a-zA-Z0-9_]{0,29}){1,10}#)\}\}`,
)

// VarRef is a single occurrence of a {{#...#}} reference inside a template.
type VarRef struct {
	// Raw is the full match (including the outer "{{#" / "#}}" delimiters).
	Raw string
	// Selector is the path split on '.' (e.g. ["start", "query"]).
	Selector []string
}

// ExtractTemplateRefs returns every {{#node.var.path#}} reference in a
// template string.
//
// This is the Go counterpart of graphon's
// VariableTemplateParser.extract_variable_selectors().
func ExtractTemplateRefs(template string) []VarRef {
	if template == "" {
		return nil
	}
	matches := templateVarRegex.FindAllStringSubmatch(template, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]VarRef, 0, len(matches))
	for _, m := range matches {
		inner := m[1]                   // "#node.var.path#"
		path := inner[1 : len(inner)-1] // strip the surrounding '#'
		out = append(out, VarRef{
			Raw:      m[0],
			Selector: strings.Split(path, "."),
		})
	}
	return out
}

// IsReservedSelector reports whether the first segment of a selector points
// at one of the reserved namespaces (sys / env / conversation / rag).
func IsReservedSelector(selector []string) bool {
	if len(selector) == 0 {
		return false
	}
	_, ok := ReservedNamespaces[selector[0]]
	return ok
}

// ParseHTTPLineMap parses Dify's "key:value\n..." string format used by
// HTTPRequestNodeData.Headers and .Params.
//
// Empty lines and lines without a colon are skipped. Whitespace around the
// key and value is trimmed.
func ParseHTTPLineMap(s string) map[string]string {
	if s == "" {
		return nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		if k == "" {
			continue
		}
		out[k] = v
	}
	return out
}
