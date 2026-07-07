package dsl

import (
	"fmt"
	"sort"
	"strings"
)

// IssueCode classifies a single validation issue.
type IssueCode string

const (
	IssueMissingNode        IssueCode = "MISSING_NODE"
	IssueInvalidRoot        IssueCode = "INVALID_ROOT"
	IssueDuplicateNodeID    IssueCode = "DUPLICATE_NODE_ID"
	IssueOrphanContainerSub IssueCode = "ORPHAN_CONTAINER_SUBGRAPH"
	IssueIterationStartMiss IssueCode = "ITERATION_START_MISSING"
	IssueLoopStartMissing   IssueCode = "LOOP_START_MISSING"
	IssueUnknownVarRef      IssueCode = "UNKNOWN_VARIABLE_REFERENCE"
	IssueUnknownNodeType    IssueCode = "UNKNOWN_NODE_TYPE"
	IssueShortSelector      IssueCode = "SHORT_VARIABLE_SELECTOR"
)

// Issue describes one validation finding.
type Issue struct {
	Code    IssueCode
	Message string
	NodeID  string
}

func (i Issue) String() string {
	if i.NodeID != "" {
		return fmt.Sprintf("[%s] %s (node=%s)", i.Code, i.Message, i.NodeID)
	}
	return fmt.Sprintf("[%s] %s", i.Code, i.Message)
}

// IsRootNodeType reports whether the given node type is allowed to be a root.
//
// Reference: api/core/workflow/node_factory.py:_START_NODE_TYPES
func IsRootNodeType(t NodeType) bool {
	switch t {
	case NodeTypeStart,
		NodeTypeDatasource,
		NodeTypeTriggerWebhook,
		NodeTypeTriggerSchedule,
		NodeTypeTriggerPlugin:
		return true
	}
	return false
}

// Validate runs all built-in graph rules and returns every issue found.
//
// The function is forgiving: it never returns an error itself; callers can
// inspect the returned slice and decide whether to treat any issue as fatal.
func (d *DSL) Validate() []Issue {
	var issues []Issue
	if !d.IsWorkflow() {
		return issues
	}
	g := &d.Workflow.Graph
	nodeIndex := buildNodeIndex(g.Nodes)

	issues = append(issues, validateUniqueIDs(g.Nodes)...)
	issues = append(issues, validateEdgesEndpoints(g.Edges, nodeIndex)...)
	issues = append(issues, validateRoots(g.Nodes)...)
	issues = append(issues, validateContainerSubgraphs(g.Nodes, nodeIndex)...)
	issues = append(issues, validateVariableReferences(g.Nodes, nodeIndex,
		d.Workflow.EnvironmentVariables, d.Workflow.ConversationVariables)...)

	// Sort for stable output.
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].NodeID < issues[j].NodeID
	})
	return issues
}

// buildNodeIndex returns a map of node ID -> node, excluding canvas notes.
func buildNodeIndex(nodes []Node) map[string]*Node {
	idx := make(map[string]*Node, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		if n.IsNote() {
			continue
		}
		idx[n.ID] = n
	}
	return idx
}

func validateUniqueIDs(nodes []Node) []Issue {
	seen := map[string]int{}
	var issues []Issue
	for _, n := range nodes {
		if n.IsNote() {
			continue
		}
		seen[n.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			issues = append(issues, Issue{
				Code:    IssueDuplicateNodeID,
				NodeID:  id,
				Message: fmt.Sprintf("node id %q appears %d times", id, count),
			})
		}
	}
	return issues
}

func validateEdgesEndpoints(edges []Edge, idx map[string]*Node) []Issue {
	var issues []Issue
	for _, e := range edges {
		if _, ok := idx[e.Source]; !ok {
			issues = append(issues, Issue{
				Code:    IssueMissingNode,
				NodeID:  e.Source,
				Message: fmt.Sprintf("edge %s references unknown source node %q", e.ID, e.Source),
			})
		}
		if _, ok := idx[e.Target]; !ok {
			issues = append(issues, Issue{
				Code:    IssueMissingNode,
				NodeID:  e.Target,
				Message: fmt.Sprintf("edge %s references unknown target node %q", e.ID, e.Target),
			})
		}
	}
	return issues
}

func validateRoots(nodes []Node) []Issue {
	hasRoot := false
	for _, n := range nodes {
		if n.IsNote() {
			continue
		}
		if IsRootNodeType(n.Data.NodeType()) {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		return []Issue{{
			Code:    IssueInvalidRoot,
			Message: "graph has no root node (start / datasource / trigger-*)",
		}}
	}
	return nil
}

func validateContainerSubgraphs(nodes []Node, idx map[string]*Node) []Issue {
	var issues []Issue
	for _, n := range nodes {
		switch t := n.Data.(type) {
		case *IterationNodeData:
			if t.StartNodeID == "" {
				continue
			}
			child, ok := idx[t.StartNodeID]
			if !ok {
				issues = append(issues, Issue{
					Code:    IssueIterationStartMiss,
					NodeID:  n.ID,
					Message: fmt.Sprintf("iteration start_node_id=%q points to a missing node", t.StartNodeID),
				})
				continue
			}
			if child.Data.NodeType() != NodeTypeIterationStart {
				issues = append(issues, Issue{
					Code:   IssueIterationStartMiss,
					NodeID: n.ID,
					Message: fmt.Sprintf("iteration start_node_id=%q is %q, expected %q",
						t.StartNodeID, child.Data.NodeType(), NodeTypeIterationStart),
				})
			}
		case *LoopNodeData:
			if t.StartNodeID == "" {
				continue
			}
			child, ok := idx[t.StartNodeID]
			if !ok {
				issues = append(issues, Issue{
					Code:    IssueLoopStartMissing,
					NodeID:  n.ID,
					Message: fmt.Sprintf("loop start_node_id=%q points to a missing node", t.StartNodeID),
				})
				continue
			}
			if child.Data.NodeType() != NodeTypeLoopStart {
				issues = append(issues, Issue{
					Code:   IssueLoopStartMissing,
					NodeID: n.ID,
					Message: fmt.Sprintf("loop start_node_id=%q is %q, expected %q",
						t.StartNodeID, child.Data.NodeType(), NodeTypeLoopStart),
				})
			}
		}
	}
	return issues
}

// validateVariableReferences walks every place where a variable selector or
// {{#...#}} template appears and verifies the referenced node ID either exists
// in the graph or belongs to a reserved namespace.
func validateVariableReferences(
	nodes []Node,
	idx map[string]*Node,
	envVars, convVars []ScopedVariable,
) []Issue {
	known := map[string]struct{}{}
	for id := range idx {
		known[id] = struct{}{}
	}
	for ns := range ReservedNamespaces {
		known[ns] = struct{}{}
	}
	// Also accept user-declared scoped variables.
	_ = envVars
	_ = convVars

	var issues []Issue
	for _, n := range nodes {
		if n.IsNote() {
			continue
		}
		for _, ref := range CollectNodeReferences(&n) {
			if len(ref.Selector) < MinSelectorLength {
				issues = append(issues, Issue{
					Code:   IssueShortSelector,
					NodeID: n.ID,
					Message: fmt.Sprintf("selector %v has fewer than %d segments",
						ref.Selector, MinSelectorLength),
				})
				continue
			}
			head := ref.Selector[0]
			if _, ok := known[head]; !ok {
				issues = append(issues, Issue{
					Code:   IssueUnknownVarRef,
					NodeID: n.ID,
					Message: fmt.Sprintf("references unknown node %q (selector=%v, raw=%q)",
						head, ref.Selector, ref.Raw),
				})
			}
		}
	}
	return issues
}

// CollectNodeReferences returns every variable reference declared by a node,
// covering both structured selectors (value_selector / variable_selector / ...)
// and {{#...#}} templates embedded in string fields.
//
// The implementation switches on the concrete NodeData type; new node types
// should add their own cases here.
func CollectNodeReferences(n *Node) []VarRef {
	var out []VarRef
	push := func(sel []string, raw string) {
		if len(sel) == 0 {
			return
		}
		out = append(out, VarRef{Raw: raw, Selector: append([]string(nil), sel...)})
	}
	pushTemplate := func(s string) {
		if s == "" {
			return
		}
		out = append(out, ExtractTemplateRefs(s)...)
	}

	switch d := n.Data.(type) {
	case *EndNodeData:
		for _, o := range d.Outputs {
			push(o.ValueSelector, "end.outputs."+o.Variable)
		}
	case *AnswerNodeData:
		pushTemplate(d.Answer)
	case *LLMNodeData:
		if d.Context.VariableSelector != nil {
			push(d.Context.VariableSelector, "llm.context")
		}
		if d.Vision.Configs != nil {
			push(d.Vision.Configs.VariableSelector, "llm.vision")
		}
		if msgs, ok := d.PromptMessages(); ok {
			for _, m := range msgs {
				pushTemplate(m.Text)
				pushTemplate(m.Jinja2Text)
			}
		}
		if c, ok := d.PromptCompletion(); ok {
			pushTemplate(c.Text)
			pushTemplate(c.Jinja2Text)
		}
		if d.Memory != nil {
			pushTemplate(d.Memory.QueryPromptTemplate)
		}
		if d.PromptConfig != nil {
			for _, j := range d.PromptConfig.Jinja2Variables {
				push(j.ValueSelector, "llm.jinja2_var."+j.Variable)
			}
		}
	case *IfElseNodeData:
		for _, c := range d.IterCases() {
			for _, cond := range c.Conditions {
				push(cond.VariableSelector, "if-else.case."+c.CaseID)
			}
		}
	case *CodeNodeData:
		for _, v := range d.Variables {
			push(v.ValueSelector, "code.var."+v.Variable)
		}
		// code field itself can contain {{#...#}} but it's typically processed
		// by the runtime, not the static parser.
	case *TemplateTransformNodeData:
		for _, v := range d.Variables {
			push(v.ValueSelector, "template.var."+v.Variable)
		}
		pushTemplate(d.Template)
	case *HTTPRequestNodeData:
		pushTemplate(d.URL)
		pushTemplate(d.Headers)
		pushTemplate(d.Params)
		if d.Body != nil {
			for _, b := range d.Body.Data {
				pushTemplate(b.Value)
				if len(b.File) > 0 {
					push(b.File, "http.body.file")
				}
			}
		}
		if d.Authorization.Config != nil {
			pushTemplate(d.Authorization.Config.APIKey)
		}
	case *ToolNodeData:
		for name, p := range d.ToolParameters {
			switch p.Type {
			case "variable":
				if sel, ok := anyToStringSlice(p.Value); ok {
					push(sel, "tool.param."+name)
				}
			case "mixed":
				if s, ok := p.Value.(string); ok {
					pushTemplate(s)
				}
			}
		}
	case *VariableAggregatorNodeData:
		for _, sel := range d.Variables {
			push(sel, "aggregator.var")
		}
		if d.AdvancedSettings != nil {
			for _, g := range d.AdvancedSettings.Groups {
				for _, sel := range g.Variables {
					push(sel, "aggregator.group."+g.GroupName)
				}
			}
		}
	case *VariableAssignerNodeData:
		for _, it := range d.Items {
			push(it.VariableSelector, "assigner.target")
			if it.InputType == "variable" {
				if sel, ok := anyToStringSlice(it.Value); ok {
					push(sel, "assigner.source")
				}
			}
		}
		if len(d.AssignedVariableSelector) > 0 {
			push(d.AssignedVariableSelector, "assigner.legacy.target")
		}
		if len(d.InputVariableSelector) > 0 {
			push(d.InputVariableSelector, "assigner.legacy.source")
		}
		for _, sel := range d.Variables {
			push(sel, "assigner.legacy.aggregator")
		}
	case *IterationNodeData:
		push(d.IteratorSelector, "iteration.iterator")
		push(d.OutputSelector, "iteration.output")
	case *LoopNodeData:
		for _, c := range d.BreakConditions {
			push(c.VariableSelector, "loop.break")
		}
	case *ListOperatorNodeData:
		push(d.Variable, "list-operator.variable")
	case *DocumentExtractorNodeData:
		push(d.VariableSelector, "document-extractor.variable")
	case *ParameterExtractorNodeData:
		push(d.Query, "parameter-extractor.query")
	case *QuestionClassifierNodeData:
		push(d.QueryVariableSelector, "question-classifier.query")
	case *KnowledgeRetrievalNodeData:
		if sel, ok := anyToStringSlice(d.QueryVariableSelector); ok {
			push(sel, "knowledge-retrieval.query")
		}
	case *HumanInputNodeData:
		pushTemplate(d.FormContent)
		for _, in := range d.Inputs {
			if in.Default != nil && in.Default.Type == "variable" {
				push(in.Default.Selector, "human-input.default."+in.OutputVariableName)
			}
		}
	case *AgentNodeData:
		for name, p := range d.AgentParameters {
			switch p.Type {
			case "variable":
				if sel, ok := anyToStringSlice(p.Value); ok {
					push(sel, "agent.param."+name)
				}
			case "mixed":
				if s, ok := p.Value.(string); ok {
					pushTemplate(s)
				}
			}
		}
	}
	return out
}

// anyToStringSlice converts a YAML-decoded any to []string, accepting both
// already-typed []string and the more common []any.
func anyToStringSlice(v any) ([]string, bool) {
	switch x := v.(type) {
	case []string:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

// FormatIssues returns a multi-line, human-readable summary of issues.
func FormatIssues(issues []Issue) string {
	if len(issues) == 0 {
		return "no issues"
	}
	lines := make([]string, len(issues))
	for i, x := range issues {
		lines[i] = "  - " + x.String()
	}
	return strings.Join(lines, "\n")
}
