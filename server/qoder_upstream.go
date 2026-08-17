package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type qoderUpstream struct {
	Body         map[string]interface{}
	QoderModel   string
	URL          string
	ExtraHeaders map[string]string
	ToolsEnabled bool
}

func (b *OpenAiBridge) prepareQoderUpstream(req ChatRequest) (*qoderUpstream, error) {
	use1M := modelRequests1M(req.Model, req.Context1M) || contextTokens(req.ContextSize) == 1000000
	model := req.Model
	if model == "" {
		model = getSetting("QODER_MODEL")
		if model == "" {
			model = "lite"
		}
	}
	mapping := b.mappingSnapshot()
	if qoderKey, ok := mapping[model]; ok {
		model = qoderKey
	} else {
		model, _ = stripModel1MSuffix(model)
		if qoderKey, ok := mapping[model]; ok {
			model = qoderKey
		}
	}

	max37 := model == "qmodel_latest" || model == "qmodel_38max"
	template := applyTemplatePlaceholders(b.selectTemplate(model))
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(template), &body); err != nil {
		return nil, fmt.Errorf("parse baseprompt: %w", err)
	}

	nid := generateUUID()
	body["request_id"] = nid
	body["chat_record_id"] = nid
	body["request_set_id"] = generateUUID()
	body["session_id"] = generateUUID()
	body["stream"] = true
	body["aliyun_user_type"] = b.sess.Identity.UserType

	mc := body["model_config"].(map[string]interface{})
	mc["key"] = model
	mc["display_name"] = displayNameForModel(model)
	mc["is_vl"] = modelSupportsVision(model)
	if tokens := contextTokens(req.ContextSize); use1M || tokens > 0 {
		if use1M {
			tokens = 1000000
		}
		mc["max_input_tokens"] = float64(tokens)
	}

	ctx := body["chat_context"].(map[string]interface{})
	normalizeChatContext(ctx, max37)
	if extra, ok := ctx["extra"].(map[string]interface{}); ok {
		if mcfg, ok := extra["modelConfig"].(map[string]interface{}); ok {
			mcfg["key"] = model
			if req.Thinking != "" {
				mcfg["is_reasoning"] = req.Thinking != "off"
			}
		}
	}

	// Wire format from 1608.har: parameters{reasoning_effort, enable_thinking,
	// context_length} + model_config.is_reasoning/max_input_tokens.
	if params, ok := body["parameters"].(map[string]interface{}); ok {
		if final, ok := mc["max_input_tokens"].(float64); ok && final > 0 {
			params["context_length"] = final
		}
		if req.Thinking != "" {
			on := req.Thinking != "off"
			mc["is_reasoning"] = on
			params["enable_thinking"] = on
			if on {
				if effort := strings.ToLower(req.Thinking); effort != "on" && effort != "enabled" {
					params["reasoning_effort"] = effort
				}
			} else {
				delete(params, "reasoning_effort")
			}
		}
	}

	biz := body["business"].(map[string]interface{})
	biz["id"] = generateUUID()
	biz["begin_at"] = time.Now().UnixMilli()

	prompt := extractLatestUserPrompt(req.Messages)
	latestUser, hasLatestUser := findLatestUserMessage(req.Messages)
	if max37 {
		if hasLatestUser {
			applyMax37UserPrompt(ctx, latestUser)
		}
	} else {
		applyLiteUserPrompt(ctx, prompt)
	}
	bizName := prompt
	if len(prompt) > 30 {
		bizName = prompt[:30]
	}
	biz["name"] = bizName

	toolsEnabled := len(req.Tools) > 0
	body["messages"] = buildQoderMessages(body["messages"], req.Messages, prompt, toolsEnabled, max37)

	if params, ok := body["parameters"].(map[string]interface{}); ok {
		params["max_tokens"] = maxTokensForRequest(model, req.Messages)
	}

	// Never forward the huge CLI tool catalog from the template.
	if toolsEnabled {
		body["tools"] = req.Tools
	} else {
		body["tools"] = []interface{}{}
	}

	fmt.Printf("[bridge] model=%s max37=%v context1m=%v prompt=%s\n", model, max37, use1M, truncateString(prompt, 80))

	url := "https://api1.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation?FetchKeys=llm_model_result&AgentId=agent_common&Encode=1"
	extra := map[string]string{
		"x-model-key":    model,
		"x-model-source": getString(mc, "source"),
	}
	return &qoderUpstream{
		Body:         body,
		QoderModel:   model,
		URL:          url,
		ExtraHeaders: extra,
		ToolsEnabled: toolsEnabled,
	}, nil
}

// contextTokens maps a context size label to token count (0 = template default).
func contextTokens(size string) int {
	switch strings.ToUpper(strings.TrimSpace(size)) {
	case "200K":
		return 200000
	case "400K":
		return 400000
	case "1M":
		return 1000000
	}
	return 0
}

func modelSupportsVision(model string) bool {
	// Only a few keys are known to be multimodal in this bridge.
	switch model {
	case "qmodel_latest", "qmodel_38max", "gm51model":
		return true
	default:
		return false
	}
}

func maxTokensForRequest(model string, messages []Message) int {
	// The TUI "prompt suggestion" system prompt should produce a tiny output budget.
	if model == "lite" && hasPromptSuggestionSystem(messages) {
		return 64
	}
	return 32000
}

func hasPromptSuggestionSystem(messages []Message) bool {
	for i := range messages {
		if messages[i].Role != "system" {
			continue
		}
		txt := strings.ToLower(messages[i].Text())
		if strings.Contains(txt, "next-action suggestions") {
			return true
		}
	}
	return false
}

func displayNameForModel(model string) string {
	alias, ok := reverseKnown[model]
	if !ok || alias == "" {
		return ""
	}
	return formatDisplayName(alias)
}

func formatDisplayName(alias string) string {
	parts := strings.Split(alias, "-")
	for i, p := range parts {
		lp := strings.ToLower(p)
		switch lp {
		case "glm":
			parts[i] = "GLM"
		default:
			if lp == "" {
				parts[i] = p
				continue
			}
			parts[i] = strings.ToUpper(lp[:1]) + p[1:]
		}
	}
	return strings.Join(parts, "-")
}
