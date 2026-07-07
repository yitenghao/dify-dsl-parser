package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"dify-dsl-parser/dsl"
)

func init() {
	RegisterRunner(dsl.NodeTypeLLM, runnerFunc(runLLM))
	RegisterRunner(dsl.NodeTypeHTTPRequest, runnerFunc(runHTTPRequest))
	RegisterRunner(dsl.NodeTypeTool, runnerFunc(runTool))
}

// ----------------------------------------------------------------------------
// llm
// Reference: graphon.nodes.llm.llm_node.LLMNode
// ----------------------------------------------------------------------------

func runLLM(ctx context.Context, env *RunEnv) (*RunResult, error) {
	d, ok := env.Node.Data.(*dsl.LLMNodeData)
	if !ok {
		return nil, fmt.Errorf("llm: unexpected data type %T", env.Node.Data)
	}
	if env.LLM == nil {
		return nil, errors.New("llm: no LLMClient configured (use Engine.WithLLM)")
	}

	// Render prompt template against the variable pool.
	req := LLMRequest{
		Provider: d.Model.Provider,
		Model:    d.Model.Name,
		Mode:     d.Model.Mode,
		Params:   d.Model.CompletionParams,
		Stream:   true,
		OnDelta: func(chunk string) {
			env.Emit(StreamChunk{NodeID: env.Node.ID, Delta: chunk})
		},
	}
	if req.Mode == "" {
		req.Mode = "chat"
	}

	switch req.Mode {
	case "completion":
		if c, ok := d.PromptCompletion(); ok {
			req.Prompt = RenderTemplate(c.Text, env.Pool)
		}
	default: // chat
		if msgs, ok := d.PromptMessages(); ok {
			req.Messages = make([]ChatMessage, 0, len(msgs))
			for _, m := range msgs {
				req.Messages = append(req.Messages, ChatMessage{
					Role:    m.Role,
					Content: RenderTemplate(m.Text, env.Pool),
				})
			}
		}
	}

	resp, err := env.LLM.Chat(ctx, req)
	if err != nil {
		return &RunResult{Status: StatusFailed, Error: err.Error()}, nil
	}

	outputs := map[string]any{
		"text": resp.Text,
		"usage": map[string]any{
			"prompt_tokens":     resp.Usage.PromptTokens,
			"completion_tokens": resp.Usage.CompletionTokens,
			"total_tokens":      resp.Usage.TotalTokens,
		},
		"finish_reason": resp.FinishReason,
	}
	return &RunResult{Status: StatusSucceeded, Outputs: outputs}, nil
}

// ----------------------------------------------------------------------------
// http-request
// Reference: graphon.nodes.http_request.http_request_node.HttpRequestNode
// ----------------------------------------------------------------------------

func runHTTPRequest(ctx context.Context, env *RunEnv) (*RunResult, error) {
	d, ok := env.Node.Data.(*dsl.HTTPRequestNodeData)
	if !ok {
		return nil, fmt.Errorf("http-request: unexpected data type %T", env.Node.Data)
	}

	url := RenderTemplate(d.URL, env.Pool)
	method := strings.ToUpper(d.Method)
	if method == "" {
		method = http.MethodGet
	}

	// Body
	var body io.Reader
	contentType := ""
	if d.Body != nil && d.Body.Type != "" && d.Body.Type != "none" {
		b, ct, err := buildHTTPBody(d.Body, env.Pool)
		if err != nil {
			return &RunResult{Status: StatusFailed, Error: err.Error()}, nil
		}
		body = bytes.NewReader(b)
		contentType = ct
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return &RunResult{Status: StatusFailed, Error: err.Error()}, nil
	}

	// Headers (rendered with templates) + content type
	for k, v := range dsl.ParseHTTPLineMap(RenderTemplate(d.Headers, env.Pool)) {
		req.Header.Set(k, v)
	}
	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}

	// Query params
	if params := dsl.ParseHTTPLineMap(RenderTemplate(d.Params, env.Pool)); len(params) > 0 {
		q := req.URL.Query()
		for k, v := range params {
			q.Set(k, v)
		}
		req.URL.RawQuery = q.Encode()
	}

	// Authorization
	switch d.Authorization.Type {
	case "api-key":
		if cfg := d.Authorization.Config; cfg != nil {
			key := RenderTemplate(cfg.APIKey, env.Pool)
			switch cfg.Type {
			case "bearer":
				req.Header.Set("Authorization", "Bearer "+key)
			case "basic":
				req.Header.Set("Authorization", "Basic "+key)
			case "custom":
				h := cfg.Header
				if h == "" {
					h = "Authorization"
				}
				req.Header.Set(h, key)
			default:
				req.Header.Set("Authorization", key)
			}
		}
	}

	timeout := 60 * time.Second
	if d.Timeout != nil && d.Timeout.Read != nil && *d.Timeout.Read > 0 {
		timeout = time.Duration(*d.Timeout.Read) * time.Second
	}
	client := &http.Client{Timeout: timeout}

	resp, err := client.Do(req)
	if err != nil {
		return &RunResult{Status: StatusFailed, Error: err.Error()}, nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &RunResult{Status: StatusFailed, Error: err.Error()}, nil
	}

	// Try to parse as JSON for the convenience of downstream nodes.
	var parsed any
	if json.Valid(respBody) {
		_ = json.Unmarshal(respBody, &parsed)
	}

	headers := map[string]any{}
	for k, v := range resp.Header {
		if len(v) == 1 {
			headers[k] = v[0]
		} else {
			headers[k] = v
		}
	}

	outputs := map[string]any{
		"status_code": resp.StatusCode,
		"body":        string(respBody),
		"headers":     headers,
	}
	if parsed != nil {
		outputs["body_json"] = parsed
	}
	return &RunResult{Status: StatusSucceeded, Outputs: outputs}, nil
}

// buildHTTPBody constructs the request body bytes plus a content-type.
func buildHTTPBody(b *dsl.HTTPRequestBody, pool *VariablePool) ([]byte, string, error) {
	switch b.Type {
	case "json":
		// json bodies typically have a single text entry whose value is a
		// JSON string. Render it through the template engine.
		if len(b.Data) == 0 {
			return []byte("null"), "application/json", nil
		}
		raw := b.Data[0].Value
		rendered := RenderTemplate(raw, pool)
		return []byte(rendered), "application/json", nil
	case "raw-text":
		if len(b.Data) == 0 {
			return nil, "text/plain", nil
		}
		return []byte(RenderTemplate(b.Data[0].Value, pool)), "text/plain", nil
	case "x-www-form-urlencoded", "form-data":
		// Minimal form support: percent-encoded key=value pairs.
		var buf bytes.Buffer
		for i, e := range b.Data {
			if i > 0 {
				buf.WriteByte('&')
			}
			buf.WriteString(e.Key)
			buf.WriteByte('=')
			buf.WriteString(RenderTemplate(e.Value, pool))
		}
		return buf.Bytes(), "application/x-www-form-urlencoded", nil
	}
	return nil, "", fmt.Errorf("unsupported body type %q", b.Type)
}

// ----------------------------------------------------------------------------
// tool
// Reference: graphon.nodes.tool.tool_node.ToolNode
// ----------------------------------------------------------------------------

func runTool(ctx context.Context, env *RunEnv) (*RunResult, error) {
	d, ok := env.Node.Data.(*dsl.ToolNodeData)
	if !ok {
		return nil, fmt.Errorf("tool: unexpected data type %T", env.Node.Data)
	}
	if env.Tool == nil {
		return nil, errors.New("tool: no ToolClient configured (use Engine.WithTool)")
	}

	// Resolve tool parameters: variable-bound values come from the pool,
	// template-bound values are rendered, constants pass through.
	params := map[string]any{}
	for name, p := range d.ToolParameters {
		switch p.Type {
		case "variable":
			if sel, ok := p.Value.([]any); ok {
				selStr := make([]string, 0, len(sel))
				for _, s := range sel {
					selStr = append(selStr, fmt.Sprint(s))
				}
				if v, found := env.Pool.Get(selStr); found {
					params[name] = v
				}
			}
		case "mixed":
			if s, ok := p.Value.(string); ok {
				params[name] = RenderTemplate(s, env.Pool)
			} else {
				params[name] = p.Value
			}
		default:
			params[name] = p.Value
		}
	}

	resp, err := env.Tool.Invoke(ctx, ToolRequest{
		ProviderID:     d.ProviderID,
		ProviderType:   d.ProviderType,
		ToolName:       d.ToolName,
		Configurations: d.ToolConfigurations,
		Parameters:     params,
		CredentialID:   d.CredentialID,
	})
	if err != nil {
		return &RunResult{Status: StatusFailed, Error: err.Error()}, nil
	}
	outs := map[string]any{}
	for k, v := range resp.Result {
		outs[k] = v
	}
	if resp.Text != "" {
		outs["text"] = resp.Text
	}
	return &RunResult{Status: StatusSucceeded, Outputs: outs}, nil
}
