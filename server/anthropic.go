package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type anthropicRequest struct {
	Model     string          `json:"model"`
	Messages  []anthropicMsg  `json:"messages"`
	System    json.RawMessage `json:"system"`
	Tools     []anthropicTool `json:"tools"`
	Stream    bool            `json:"stream"`
	MaxTokens int             `json:"max_tokens"`
}

type anthropicMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

func anthropicToChatRequest(req anthropicRequest) ChatRequest {
	msgs := make([]Message, 0, len(req.Messages)+1)
	if sys := anthropicSystemText(req.System); sys != "" {
		msgs = append(msgs, Message{Role: "system", Content: mustRawJSON(sys)})
	}
	for _, m := range req.Messages {
		msgs = append(msgs, anthropicMsgToOpenAI(m)...)
	}
	tools := make([]Tool, 0, len(req.Tools))
	for _, t := range req.Tools {
		params := t.InputSchema
		if params == nil {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		tools = append(tools, Tool{
			Type: "function",
			Function: FunctionDef{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return ChatRequest{
		Model:    req.Model,
		Messages: msgs,
		Tools:    tools,
		Stream:   req.Stream,
	}
}

func anthropicSystemText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var parts []map[string]interface{}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		if t, _ := p["type"].(string); t == "text" {
			if text, _ := p["text"].(string); text != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(text)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func anthropicMsgToOpenAI(m anthropicMsg) []Message {
	role := m.Role
	if role == "" {
		role = "user"
	}
	var asString string
	if err := json.Unmarshal(m.Content, &asString); err == nil {
		return []Message{{Role: role, Content: mustRawJSON(asString)}}
	}
	var blocks []map[string]interface{}
	if err := json.Unmarshal(m.Content, &blocks); err != nil {
		return []Message{{Role: role, Content: m.Content}}
	}
	if role == "assistant" {
		return anthropicAssistantToOpenAI(blocks)
	}
	return anthropicUserToOpenAI(role, blocks)
}

func anthropicAssistantToOpenAI(blocks []map[string]interface{}) []Message {
	var textParts []string
	toolCalls := []map[string]interface{}{}
	for _, block := range blocks {
		switch block["type"] {
		case "text":
			if t, _ := block["text"].(string); t != "" {
				textParts = append(textParts, t)
			}
		case "tool_use":
			name, _ := block["name"].(string)
			id, _ := block["id"].(string)
			args, _ := json.Marshal(block["input"])
			toolCalls = append(toolCalls, map[string]interface{}{
				"id":   id,
				"type": "function",
				"function": map[string]interface{}{
					"name":      name,
					"arguments": string(args),
				},
			})
		}
	}
	content := strings.Join(textParts, "\n")
	msg := Message{Role: "assistant", Content: mustRawJSON(content)}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	return []Message{msg}
}

func anthropicUserToOpenAI(role string, blocks []map[string]interface{}) []Message {
	out := []Message{}
	var textParts []string
	flush := func() {
		if len(textParts) == 0 {
			return
		}
		out = append(out, Message{Role: role, Content: mustRawJSON(strings.Join(textParts, "\n"))})
		textParts = nil
	}
	for _, block := range blocks {
		switch block["type"] {
		case "text":
			if t, _ := block["text"].(string); t != "" {
				textParts = append(textParts, t)
			}
		case "tool_result":
			flush()
			id, _ := block["tool_use_id"].(string)
			out = append(out, Message{
				Role:       "tool",
				Content:    mustRawJSON(toolResultContent(block["content"])),
				ToolCallID: id,
			})
		}
	}
	flush()
	if len(out) == 0 {
		out = append(out, Message{Role: role, Content: mustRawJSON("")})
	}
	return out
}

func toolResultContent(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case []interface{}:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			if m, ok := item.(map[string]interface{}); ok {
				if text, _ := m["text"].(string); text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func mustRawJSON(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}

func openAIToolsToAnthropic(tools []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tc := range tools {
		id, _ := tc["id"].(string)
		fn, _ := tc["function"].(map[string]interface{})
		name, _ := fn["name"].(string)
		argsRaw, _ := fn["arguments"].(string)
		var input interface{} = map[string]interface{}{}
		if argsRaw != "" {
			_ = json.Unmarshal([]byte(argsRaw), &input)
		}
		out = append(out, map[string]interface{}{
			"type":  "tool_use",
			"id":    id,
			"name":  name,
			"input": input,
		})
	}
	return out
}

func writeAnthropicJSONError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"type": "error",
		"error": map[string]interface{}{
			"type":    errType,
			"message": message,
		},
	})
}

func anthropicStopReason(finish string) string {
	if finish == "tool_calls" {
		return "tool_use"
	}
	if finish == "length" {
		return "max_tokens"
	}
	return "end_turn"
}

func anthropicMsgID() string {
	return "msg_" + strings.ReplaceAll(generateUUID(), "-", "")[:24]
}

func writeAnthropicSSE(w http.ResponseWriter, event string, payload interface{}) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, toJson(payload))
}
