package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"qoder2api/logx"
)

func (b *OpenAiBridge) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req anthropicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAnthropicJSONError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	chat := anthropicToChatRequest(req)
	_, chat.Context1M = stripModel1MSuffix(req.Model)
	logx.Infof("[anthropic] model=%s msgs=%d tools=%d stream=%v\n",
		req.Model, len(chat.Messages), len(chat.Tools), req.Stream)

	up, err := b.prepareQoderUpstream(chat)
	if err != nil {
		writeAnthropicJSONError(w, http.StatusInternalServerError, "api_error", err.Error())
		return
	}

	msgID := anthropicMsgID()
	outModel := req.Model
	if outModel == "" {
		outModel = chat.Model
	}

	if req.Stream {
		b.streamAnthropic(w, up, msgID, outModel)
		return
	}
	b.completeAnthropic(w, up, msgID, outModel)
}

func (b *OpenAiBridge) completeAnthropic(w http.ResponseWriter, up *qoderUpstream, msgID, outModel string) {
	full := &strings.Builder{}
	reasoning := &strings.Builder{}
	toolCalls := newToolCallAccumulator()
	var upstreamFinish string

	err := b.client().OpenStreamLines(up.URL, up.Body, up.ExtraHeaders, func(line string) error {
		if !strings.HasPrefix(line, "data:") {
			return nil
		}
		payload := strings.TrimSpace(line[5:])
		if apiErr := parseQoderStreamError(payload); apiErr != nil {
			return apiErr
		}
		delta := extractDelta(payload)
		if fr := delta.FinishReason(); fr != "" {
			upstreamFinish = fr
		}
		if r := delta.Reasoning(); r != "" {
			reasoning.WriteString(r)
		}
		if delta.Content() != "" {
			full.WriteString(delta.Content())
		}
		if len(delta.ToolCalls()) > 0 {
			toolCalls.append(delta.ToolCalls())
		}
		return nil
	})
	if err != nil {
		if apiErr, ok := err.(*QoderAPIError); ok {
			writeAnthropicJSONError(w, apiErr.HTTPStatus(), apiErr.OpenAIType(), apiErr.Error())
			return
		}
		writeAnthropicJSONError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}

	content := []map[string]interface{}{}
	stop := anthropicStopReason(upstreamFinish)
	if !toolCalls.isEmpty() {
		content = append(content, openAIToolsToAnthropic(toolCalls.snapshot())...)
		stop = "tool_use"
	} else if up.ToolsEnabled {
		if parsed := parseToolCallsText(full.String()); parsed != nil {
			content = append(content, openAIToolsToAnthropic(parsed)...)
			stop = "tool_use"
		}
	}
	if full.Len() > 0 && stop != "tool_use" {
		content = append([]map[string]interface{}{{
			"type": "text",
			"text": full.String(),
		}}, content...)
	} else if full.Len() > 0 && stop == "tool_use" {
		content = append([]map[string]interface{}{{
			"type": "text",
			"text": full.String(),
		}}, content...)
	}
	if len(content) == 0 {
		content = []map[string]interface{}{{"type": "text", "text": ""}}
	}
	if reasoning.Len() > 0 {
		content = append([]map[string]interface{}{{
			"type":     "thinking",
			"thinking": reasoning.String(),
		}}, content...)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"id":            msgID,
		"type":          "message",
		"role":          "assistant",
		"model":         outModel,
		"content":       content,
		"stop_reason":   stop,
		"stop_sequence": nil,
		"usage": map[string]interface{}{
			"input_tokens":  0,
			"output_tokens": 0,
		},
	})
}

func (b *OpenAiBridge) streamAnthropic(w http.ResponseWriter, up *qoderUpstream, msgID, outModel string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAnthropicJSONError(w, http.StatusInternalServerError, "api_error", "streaming not supported")
		return
	}

	writeAnthropicSSE(w, "message_start", map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            msgID,
			"type":          "message",
			"role":          "assistant",
			"model":         outModel,
			"content":       []interface{}{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage":         map[string]interface{}{"input_tokens": 0, "output_tokens": 0},
		},
	})
	flusher.Flush()

	writer := &anthropicStreamWriter{w: w, flusher: flusher}
	toolAcc := newToolCallAccumulator()
	var upstreamFinish string

	err := b.client().OpenStreamLines(up.URL, up.Body, up.ExtraHeaders, func(line string) error {
		if !strings.HasPrefix(line, "data:") {
			return nil
		}
		payload := strings.TrimSpace(line[5:])
		if apiErr := parseQoderStreamError(payload); apiErr != nil {
			return apiErr
		}
		delta := extractDelta(payload)
		if fr := delta.FinishReason(); fr != "" {
			upstreamFinish = fr
		}
		if r := delta.Reasoning(); r != "" {
			writer.onThinking(r)
		}
		if delta.Content() != "" {
			writer.onText(delta.Content())
		}
		if len(delta.ToolCalls()) > 0 {
			toolAcc.append(delta.ToolCalls())
		}
		return nil
	})

	if err != nil {
		if apiErr, ok := err.(*QoderAPIError); ok {
			writeAnthropicSSE(w, "error", map[string]interface{}{
				"type": "error",
				"error": map[string]interface{}{
					"type":    apiErr.OpenAIType(),
					"message": apiErr.Error(),
				},
			})
			flusher.Flush()
			return
		}
		writeAnthropicSSE(w, "error", map[string]interface{}{
			"type": "error",
			"error": map[string]interface{}{
				"type":    "api_error",
				"message": err.Error(),
			},
		})
		flusher.Flush()
		return
	}

	tools := toolAcc.snapshot()
	if len(tools) == 0 && up.ToolsEnabled && writer.text != "" {
		if parsed := parseToolCallsText(writer.text); parsed != nil {
			tools = parsed
			writer.discardTextAsTools()
		}
	}

	stop := anthropicStopReason(upstreamFinish)
	if len(tools) > 0 {
		stop = "tool_use"
		writer.emitToolUses(tools)
	}
	writer.finish(stop)
}

type anthropicStreamWriter struct {
	w               http.ResponseWriter
	flusher         http.Flusher
	textStarted     bool
	textIndex       int
	nextIndex       int
	thinkingStarted bool
	thinkingIndex   int
	text            string
	textEmitted     bool
}

// onThinking streams a thinking delta as an Anthropic thinking block
// (opened lazily before any text/tool block, per Claude extended thinking).
func (a *anthropicStreamWriter) onThinking(chunk string) {
	if chunk == "" {
		return
	}
	if !a.thinkingStarted {
		a.thinkingIndex = a.nextIndex
		a.nextIndex++
		writeAnthropicSSE(a.w, "content_block_start", map[string]interface{}{
			"type":  "content_block_start",
			"index": a.thinkingIndex,
			"content_block": map[string]interface{}{
				"type":     "thinking",
				"thinking": "",
			},
		})
		a.thinkingStarted = true
		a.flusher.Flush()
	}
	writeAnthropicSSE(a.w, "content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": a.thinkingIndex,
		"delta": map[string]interface{}{
			"type":     "thinking_delta",
			"thinking": chunk,
		},
	})
	a.flusher.Flush()
}

// closeThinking closes the open thinking block (no-op when not started).
func (a *anthropicStreamWriter) closeThinking() {
	if !a.thinkingStarted {
		return
	}
	writeAnthropicSSE(a.w, "content_block_stop", map[string]interface{}{
		"type":  "content_block_stop",
		"index": a.thinkingIndex,
	})
	a.flusher.Flush()
	a.thinkingStarted = false
}

func (a *anthropicStreamWriter) onText(chunk string) {
	if chunk == "" {
		return
	}
	a.closeThinking()
	a.text += chunk
	if !a.textStarted {
		a.textIndex = a.nextIndex
		a.nextIndex++
		writeAnthropicSSE(a.w, "content_block_start", map[string]interface{}{
			"type":  "content_block_start",
			"index": a.textIndex,
			"content_block": map[string]interface{}{
				"type": "text",
				"text": "",
			},
		})
		a.textStarted = true
		a.flusher.Flush()
	}
	writeAnthropicSSE(a.w, "content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": a.textIndex,
		"delta": map[string]interface{}{
			"type": "text_delta",
			"text": chunk,
		},
	})
	a.textEmitted = true
	a.flusher.Flush()
}

func (a *anthropicStreamWriter) discardTextAsTools() {
	// Text was tool-call JSON fallback; don't keep it as visible text in stop.
	a.text = ""
}

func (a *anthropicStreamWriter) emitToolUses(tools []map[string]interface{}) {
	a.closeThinking()
	if a.textStarted {
		writeAnthropicSSE(a.w, "content_block_stop", map[string]interface{}{
			"type":  "content_block_stop",
			"index": a.textIndex,
		})
		a.flusher.Flush()
		a.textStarted = false
	}
	for _, block := range openAIToolsToAnthropic(tools) {
		idx := a.nextIndex
		a.nextIndex++
		writeAnthropicSSE(a.w, "content_block_start", map[string]interface{}{
			"type":          "content_block_start",
			"index":         idx,
			"content_block": map[string]interface{}{"type": "tool_use", "id": block["id"], "name": block["name"], "input": map[string]interface{}{}},
		})
		inputJSON, _ := json.Marshal(block["input"])
		writeAnthropicSSE(a.w, "content_block_delta", map[string]interface{}{
			"type":  "content_block_delta",
			"index": idx,
			"delta": map[string]interface{}{
				"type":         "input_json_delta",
				"partial_json": string(inputJSON),
			},
		})
		writeAnthropicSSE(a.w, "content_block_stop", map[string]interface{}{
			"type":  "content_block_stop",
			"index": idx,
		})
		a.flusher.Flush()
	}
}

func (a *anthropicStreamWriter) finish(stop string) {
	a.closeThinking()
	if a.textStarted {
		writeAnthropicSSE(a.w, "content_block_stop", map[string]interface{}{
			"type":  "content_block_stop",
			"index": a.textIndex,
		})
		a.flusher.Flush()
	}
	writeAnthropicSSE(a.w, "message_delta", map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   stop,
			"stop_sequence": nil,
		},
		"usage": map[string]interface{}{"output_tokens": 0},
	})
	writeAnthropicSSE(a.w, "message_stop", map[string]interface{}{
		"type": "message_stop",
	})
	a.flusher.Flush()
}
