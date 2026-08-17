package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"qoder2api/api"
	"qoder2api/auth"
)

type OpenAiBridge struct {
	sess          *auth.SessionContext
	bearerClient  *api.BearerApiClient
	templateLite  string
	templateMin   string
	promptProfile string            // full | min
	mu            sync.Mutex        // guards modelMapping
	modelMapping  map[string]string // OpenAI model -> Qoder model key
}

// mappingSnapshot returns the current model mapping. Published maps are never
// mutated after publication, so callers may read the returned map freely.
func (b *OpenAiBridge) mappingSnapshot() map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.modelMapping
}

// replaceMapping swaps in a new mapping built by mergeMappingWithCatalog.
func (b *OpenAiBridge) replaceMapping(m map[string]string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.modelMapping = m
}

type ChatRequest struct {
	Messages  []Message `json:"messages"`
	Model     string    `json:"model"`
	Stream    bool      `json:"stream"`
	Tools     []Tool    `json:"tools"`
	Context1M bool      `json:"-"`
	// Runtime knobs injected by the pool from dashboard settings.
	ContextSize string `json:"context_size,omitempty"` // "" (default) | 200K | 400K | 1M
	Thinking    string `json:"thinking,omitempty"`     // "" (model default) | off | on | effort level
}

type Message struct {
	Role       string                   `json:"role"`
	Content    json.RawMessage          `json:"content"`
	ToolCalls  []map[string]interface{} `json:"tool_calls,omitempty"`
	ToolCallID string                   `json:"tool_call_id,omitempty"`
	Name       string                   `json:"name,omitempty"`
}

func (m Message) Text() string {
	if last := lastContentTextPart(m.Content); last != "" {
		return last
	}
	return normalizeMessageContent(m.Content)
}

type Tool struct {
	Type     string      `json:"type"`
	Function FunctionDef `json:"function"`
}

type FunctionDef struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ChatResponse struct {
	Id      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	Delta        Message `json:"delta"`
	FinishReason string  `json:"finish_reason"`
}

type BridgeDelta struct {
	role      string
	content   string
	toolCalls []map[string]interface{}
}

func (d *BridgeDelta) Role() string {
	return d.role
}

func (d *BridgeDelta) Content() string {
	return d.content
}

func (d *BridgeDelta) ToolCalls() []map[string]interface{} {
	return d.toolCalls
}

func (d *BridgeDelta) IsEmpty() bool {
	return d.role == "" && d.content == "" && len(d.toolCalls) == 0
}

func NewOpenAiBridge(pat string) (*OpenAiBridge, error) {
	mid := generateUUID()
	// CLI 2cli.har: MachineId == MachineToken (same UUID)
	mtoken := mid
	// uuids := generateUUID() + generateUUID()
	// uuidBytes := []byte(uuids[:50])
	// mtoken := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(uuidBytes)
	mtype := api.CosyMachineTypeCLI
	// mtype := strings.ReplaceAll(generateUUID(), "-", "")[:18]

	sigClient := api.NewSignatureApiClient(mid, mtoken, mtype)
	jt, err := sigClient.ExchangeJobToken(pat)
	if err != nil {
		return nil, err
	}

	fmt.Printf("[bridge] session for %s (%s)\n", getString(jt, "name"), getString(jt, "id"))

	identity := auth.AuthIdentity{
		Name:               getString(jt, "name"),
		Aid:                getString(jt, "id"),
		Uid:                getString(jt, "id"),
		YxUid:              "",
		OrganizationId:     "",
		OrganizationName:   "",
		UserType:           getStringOrDefault(jt, "userType", "personal_standard"),
		SecurityOauthToken: getString(jt, "securityOauthToken"),
		RefreshToken:       getString(jt, "refreshToken"),
	}

	sess, err := auth.NewSession(identity, mid, mtoken, mtype)
	if err != nil {
		return nil, err
	}

	bearerClient := api.NewBearerApiClient(sess)

	profile := strings.ToLower(strings.TrimSpace(os.Getenv("QODER_PROMPT_PROFILE")))
	if profile == "" {
		profile = "min"
	}
	if profile != "full" && profile != "min" {
		profile = "min"
	}
	fmt.Printf("[bridge] prompt profile=%s\n", profile)

	templateMin, err := os.ReadFile("baseprompt_min.json")
	if err != nil {
		return nil, err
	}

	// Full CLI-dump template is lazy: only required for QODER_PROMPT_PROFILE=full.
	var templateLite []byte
	if profile == "full" {
		if templateLite, err = os.ReadFile("baseprompt.json"); err != nil {
			return nil, fmt.Errorf("profile=full requires baseprompt.json: %w", err)
		}
	}

	modelMapping := defaultModelMapping()

	return &OpenAiBridge{
		sess:          sess,
		bearerClient:  bearerClient,
		templateLite:  string(templateLite),
		templateMin:   string(templateMin),
		promptProfile: profile,
		modelMapping:  modelMapping,
	}, nil
}

func (b *OpenAiBridge) selectTemplate(qoderModel string) string {
	if b.promptProfile == "min" {
		return b.templateMin
	}
	return b.templateLite
}

func applyTemplatePlaceholders(template string) string {
	s := template
	s = strings.ReplaceAll(s, "{UUID1}", generateUUID())
	s = strings.ReplaceAll(s, "{UUID2}", generateUUID())
	s = strings.ReplaceAll(s, "{UUID3}", generateUUID())
	s = strings.ReplaceAll(s, "{UUID4}", generateUUID())
	s = strings.ReplaceAll(s, "{UUID5}", generateUUID())
	s = strings.ReplaceAll(s, "{TIME1}", fmt.Sprintf("%d", time.Now().UnixMilli()))
	return s
}

func (b *OpenAiBridge) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	up, err := b.prepareQoderUpstream(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	reqId := "chatcmpl-" + strings.ReplaceAll(generateUUID(), "-", "")[:24]
	created := time.Now().Unix()
	model := up.QoderModel
	stream := req.Stream

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		accumulator := newStreamAccumulator(w, reqId, created, model, up.ToolsEnabled)

		err := b.bearerClient.OpenStreamLines(up.URL, up.Body, up.ExtraHeaders, func(line string) error {
			if !strings.HasPrefix(line, "data:") {
				return nil
			}
			payload := strings.TrimSpace(line[5:])
			if apiErr := parseQoderStreamError(payload); apiErr != nil {
				fmt.Printf("[bridge] qoder error: %v\n", apiErr)
				writeStreamAPIError(w, apiErr)
				flusher.Flush()
				return apiErr
			}
			delta := extractDelta(payload)
			if delta.IsEmpty() {
				return nil
			}
			accumulator.accept(delta)
			flusher.Flush()
			return nil
		})

		if err != nil {
			if _, ok := err.(*QoderAPIError); ok {
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		accumulator.flush()

		contentN, toolN := accumulator.stats()
		fmt.Printf("[bridge] stream done content_chunks=%d tool_call_chunks=%d finish=%s\n",
			contentN, toolN, accumulator.finishReason())

		doneChunk := makeChunk(reqId, created, model)
		choices := doneChunk["choices"].([]map[string]interface{})
		choices[0]["finish_reason"] = accumulator.finishReason()
		choices[0]["delta"] = map[string]interface{}{}

		w.Write([]byte("data: " + toJson(doneChunk) + "\n\n"))
		w.Write([]byte("data: [DONE]\n\n"))
	} else {
		full := &strings.Builder{}
		toolCalls := newToolCallAccumulator()

		err := b.bearerClient.OpenStreamLines(up.URL, up.Body, up.ExtraHeaders, func(line string) error {
			if !strings.HasPrefix(line, "data:") {
				return nil
			}
			payload := strings.TrimSpace(line[5:])
			if apiErr := parseQoderStreamError(payload); apiErr != nil {
				fmt.Printf("[bridge] qoder error: %v\n", apiErr)
				return apiErr
			}
			delta := extractDelta(payload)
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
				writeJSONAPIError(w, apiErr)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		fallbackToolCalls := []map[string]interface{}{}
		if toolCalls.isEmpty() && up.ToolsEnabled {
			if parsed := parseToolCallsText(full.String()); parsed != nil {
				fallbackToolCalls = parsed
			}
		}

		msg := map[string]interface{}{
			"role": "assistant",
		}
		if len(fallbackToolCalls) > 0 {
			msg["content"] = nil
			msg["tool_calls"] = fallbackToolCalls
		} else if full.Len() == 0 && !toolCalls.isEmpty() {
			msg["content"] = nil
		} else {
			msg["content"] = full.String()
		}
		if !toolCalls.isEmpty() {
			msg["tool_calls"] = toolCalls.snapshot()
		}

		finishReason := "stop"
		if !toolCalls.isEmpty() || len(fallbackToolCalls) > 0 {
			finishReason = "tool_calls"
		}

		out := map[string]interface{}{
			"id":      reqId,
			"object":  "chat.completion",
			"created": created,
			"model":   model,
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"message":       msg,
					"finish_reason": finishReason,
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     0,
				"completion_tokens": 0,
				"total_tokens":      0,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

func (b *OpenAiBridge) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := b.BuildModelsResponse()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// BuildModelsResponse builds OpenAI-compatible /v1/models payload (live when possible).
func (b *OpenAiBridge) BuildModelsResponse() map[string]interface{} {
	items := modelsFromMapping(b.mappingSnapshot())
	live := false
	if raw, err := b.bearerClient.ListModels(); err != nil {
		fmt.Printf("[bridge] live model/list failed, using static map: %v\n", err)
	} else {
		parsed := parseQoderModelList(raw)
		if len(parsed) > 0 {
			items = parsed
			merged := mergeMappingWithCatalog(b.mappingSnapshot(), parsed)
			b.replaceMapping(merged)
			live = true
			fmt.Printf("[bridge] live models=%d mapping=%d\n", len(parsed), len(merged))
		} else {
			fmt.Printf("[bridge] model/list returned no parseable models, top keys=%v\n", mapKeys(raw))
		}
	}

	response := map[string]interface{}{
		"object": "list",
		"data":   buildOpenAIModels(items),
	}
	if live {
		response["source"] = "qoder_model_list"
	} else {
		response["source"] = "static_mapping"
	}
	return response
}

// DumpModelList fetches raw Qoder model/list for debugging / mapping updates.
func (b *OpenAiBridge) DumpModelList() (map[string]interface{}, error) {
	return b.bearerClient.ListModels()
}

func (b *OpenAiBridge) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Get user status for credits info
	sigClient := api.NewSignatureApiClient(b.sess.MachineId, b.sess.MachineToken, b.sess.MachineType)
	status, err := sigClient.UserStatus(b.sess.Identity.Uid)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// Helper functions
func generateUUID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		// Fallback to time-based if random fails
		for i := range b {
			b[i] = byte(time.Now().UnixNano() + int64(i))
		}
	}
	// Set version bits for UUID v4
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func base64UrlEncode(s string) string {
	// Proper base64 url encoding
	return base64.URLEncoding.EncodeToString([]byte(s))
}

func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getStringOrDefault(m map[string]interface{}, key, defaultVal string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultVal
}

func getSetting(key string) string {
	return os.Getenv(key)
}

func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	// Simplified deep copy
	result := make(map[string]interface{})
	for k, v := range m {
		result[k] = v
	}
	return result
}

func extractLatestUserPrompt(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		if text := messages[i].Text(); text != "" {
			return text
		}
	}
	return ""
}

func lastContentTextPart(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var items []interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		return ""
	}
	for i := len(items) - 1; i >= 0; i-- {
		item, ok := items[i].(map[string]interface{})
		if !ok {
			continue
		}
		text, ok := item["text"].(string)
		if ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func normalizeMessageContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return normalizeContentValue(value)
}

func normalizeContentValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			part := normalizeContentPart(item)
			if strings.TrimSpace(part) != "" {
				parts = append(parts, part)
			}
		}
		return strings.Join(parts, "\n\n")
	case map[string]interface{}:
		return normalizeContentPart(v)
	default:
		return ""
	}
}

func normalizeContentPart(item interface{}) string {
	switch v := item.(type) {
	case string:
		return v
	case map[string]interface{}:
		if text, ok := v["text"].(string); ok {
			return text
		}
		partType, _ := v["type"].(string)
		if partType == "image_url" || partType == "input_image" {
			if imageURL, ok := v["image_url"].(map[string]interface{}); ok {
				if url, ok := imageURL["url"].(string); ok {
					return "[image] " + url
				}
			}
		}
		if nested, ok := v["content"]; ok {
			return normalizeContentValue(nested)
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
	b, _ := json.Marshal(item)
	return string(b)
}

func applyLiteUserPrompt(ctx map[string]interface{}, prompt string) {
	ctxText, ok := ctx["text"].(map[string]interface{})
	if !ok {
		return
	}
	ctxText["text"] = prompt
	ctxExtra, ok := ctx["extra"].(map[string]interface{})
	if !ok {
		return
	}
	originalContent, ok := ctxExtra["originalContent"].(map[string]interface{})
	if !ok {
		return
	}
	originalContent["text"] = prompt
}

// normalizeChatContext sets lite vs max37 shapes for chat_context fields.
func normalizeChatContext(ctx map[string]interface{}, max37 bool) {
	extra, _ := ctx["extra"].(map[string]interface{})
	if extra == nil {
		extra = map[string]interface{}{}
		ctx["extra"] = extra
	}
	if max37 {
		ctx["text"] = textAsString(ctx["text"])
		extra["originalContent"] = textAsString(extra["originalContent"])
		return
	}
	ctx["text"] = textAsObject(ctx["text"])
	extra["originalContent"] = textAsObject(extra["originalContent"])
}

func textAsString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]interface{}:
		if s, ok := t["text"].(string); ok {
			return s
		}
	}
	return ""
}

func textAsObject(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		if _, has := m["type"]; !has {
			m["type"] = "text"
		}
		if _, has := m["text"]; !has {
			m["text"] = ""
		}
		return m
	}
	s, _ := v.(string)
	return map[string]interface{}{"type": "text", "text": s}
}

func findLatestUserMessage(messages []Message) (Message, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		if strings.TrimSpace(messages[i].Text()) != "" {
			return messages[i], true
		}
	}
	return Message{}, false
}

func findLatestUserIndex(messages []Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" && strings.TrimSpace(messages[i].Text()) != "" {
			return i
		}
	}
	return -1
}

func parseContentParts(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var items []interface{}
	if err := json.Unmarshal(raw, &items); err != nil {
		text := strings.TrimSpace(normalizeMessageContent(raw))
		if text == "" {
			return nil
		}
		return []string{text}
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, normalizeContentPart(item))
	}
	return parts
}

func buildMax37UserMessage(msg Message) map[string]interface{} {
	parts := parseContentParts(msg.Content)
	contents := make([]interface{}, 0, len(parts))
	for i, part := range parts {
		item := map[string]interface{}{
			"type": "text",
			"text": part,
		}
		if i == len(parts)-1 {
			item["cache_control"] = map[string]interface{}{"type": "ephemeral"}
		}
		contents = append(contents, item)
	}
	if len(parts) == 0 {
		text := strings.TrimSpace(msg.Text())
		parts = []string{text}
		contents = []interface{}{
			map[string]interface{}{
				"type":          "text",
				"text":          text,
				"cache_control": map[string]interface{}{"type": "ephemeral"},
			},
		}
	}
	return map[string]interface{}{
		"role":                        "user",
		"content":                     strings.Join(parts, ""),
		"contents":                    contents,
		"response_meta":               blankResponseMeta(),
		"reasoning_content_signature": "",
	}
}

func applyMax37UserPrompt(ctx map[string]interface{}, latestUser Message) {
	userMsg := buildMax37UserMessage(latestUser)
	fullContent, _ := userMsg["content"].(string)
	if ctxExtra, ok := ctx["extra"].(map[string]interface{}); ok {
		ctxExtra["originalContent"] = fullContent
	}
	ctx["text"] = fullContent
}

func buildQoderMessages(baseMessages interface{}, openAiMessages []Message, prompt string, toolsEnabled bool, max37 bool) interface{} {
	if max37 {
		return buildMax37Messages(baseMessages, openAiMessages, toolsEnabled)
	}

	rebuilt := []interface{}{}
	templateArr, _ := baseMessages.([]interface{})
	keepTemplateSystem := !hasMessageRole(openAiMessages, "system")
	if keepTemplateSystem {
		for _, raw := range templateArr {
			msg, ok := raw.(map[string]interface{})
			if ok && msg["role"] == "system" {
				rebuilt = append(rebuilt, msg)
			}
		}
	}
	for i, message := range openAiMessages {
		allowStructured := toolsEnabled && hasResolvedToolResponse(openAiMessages, i)
		converted := convertIncomingMessage(message, toolsEnabled, allowStructured)
		if converted != nil {
			rebuilt = append(rebuilt, converted)
		}
	}
	if len(rebuilt) == 0 && prompt != "" {
		rebuilt = append(rebuilt, buildUserMessage(prompt))
	}
	return rebuilt
}

func buildMax37Messages(baseMessages interface{}, openAiMessages []Message, toolsEnabled bool) interface{} {
	baseArr, ok := baseMessages.([]interface{})
	if !ok {
		return baseMessages
	}

	systems := []interface{}{}
	for _, raw := range baseArr {
		msg, ok := raw.(map[string]interface{})
		if ok && msg["role"] == "system" {
			systems = append(systems, msg)
		}
	}

	latestIdx := findLatestUserIndex(openAiMessages)
	history := []interface{}{}
	for i, message := range openAiMessages {
		if i == latestIdx {
			continue
		}
		allowStructured := toolsEnabled && hasResolvedToolResponse(openAiMessages, i)
		if converted := convertIncomingMessage(message, toolsEnabled, allowStructured); converted != nil {
			history = append(history, converted)
		}
	}

	result := make([]interface{}, 0, len(systems)+len(history)+1)
	result = append(result, systems...)
	result = append(result, history...)
	if latestIdx >= 0 {
		result = append(result, buildMax37UserMessage(openAiMessages[latestIdx]))
	}
	if len(result) == 0 {
		return baseMessages
	}
	return result
}

func hasResolvedToolResponse(messages []Message, assistantIndex int) bool {
	if assistantIndex >= len(messages) || messages[assistantIndex].Role != "assistant" {
		return false
	}
	msg := messages[assistantIndex]
	hasToolCalls := len(msg.ToolCalls) > 0 || parseToolCallsText(msg.Text()) != nil
	if !hasToolCalls {
		return false
	}
	for i := assistantIndex + 1; i < len(messages); i++ {
		switch messages[i].Role {
		case "tool":
			return true
		case "assistant", "user", "system":
			return false
		}
	}
	return false
}

func convertIncomingMessage(message Message, toolsEnabled bool, allowStructuredToolCalls bool) map[string]interface{} {
	text := strings.TrimSpace(message.Text())
	anyToolCalls := extractAnyToolCalls(message, text, toolsEnabled)
	structuredToolCalls := resolveStructuredToolCalls(message, text, toolsEnabled, allowStructuredToolCalls)

	if message.Role == "assistant" && structuredToolCalls != nil {
		return buildAssistantToolCallMessage(text, structuredToolCalls)
	}
	if message.Role == "assistant" && anyToolCalls != nil && !allowStructuredToolCalls {
		return buildStructuredMessage("assistant", summarizeUnresolvedToolCalls(anyToolCalls))
	}
	if !toolsEnabled && len(message.ToolCalls) > 0 {
		text = joinSections(text, renderToolCalls(message.ToolCalls))
	}
	if message.Role == "tool" {
		if toolsEnabled {
			return buildToolMessage(message, text)
		}
		return buildStructuredMessage("user", renderToolResult(message, text))
	}
	if text == "" {
		return nil
	}
	if message.Role == "user" {
		return buildUserMessage(text)
	}
	return buildStructuredMessage(message.Role, text)
}

func resolveStructuredToolCalls(message Message, text string, toolsEnabled bool, allowStructuredToolCalls bool) []map[string]interface{} {
	if !toolsEnabled || !allowStructuredToolCalls {
		return nil
	}
	return extractAnyToolCalls(message, text, true)
}

func extractAnyToolCalls(message Message, text string, toolsEnabled bool) []map[string]interface{} {
	if !toolsEnabled {
		return nil
	}
	if len(message.ToolCalls) > 0 {
		return normalizeToolCalls(message.ToolCalls)
	}
	return parseToolCallsText(text)
}

func buildAssistantToolCallMessage(text string, toolCalls []map[string]interface{}) map[string]interface{} {
	content := text
	if parseToolCallsText(content) != nil {
		content = ""
	}
	out := buildStructuredMessage("assistant", content)
	out["tool_calls"] = toolCalls
	return out
}

func buildToolMessage(message Message, text string) map[string]interface{} {
	out := buildStructuredMessage("tool", text)
	if message.Name != "" {
		out["name"] = message.Name
	}
	if message.ToolCallID != "" {
		out["tool_call_id"] = message.ToolCallID
	}
	return out
}

func buildUserMessage(text string) map[string]interface{} {
	return map[string]interface{}{
		"role":    "user",
		"content": "",
		"contents": []interface{}{
			map[string]interface{}{
				"type": "text",
				"text": text,
			},
		},
		"response_meta":               blankResponseMeta(),
		"reasoning_content_signature": "",
	}
}

func blankResponseMeta() map[string]interface{} {
	return map[string]interface{}{
		"id": "",
		"usage": map[string]interface{}{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
			"completion_tokens_details": map[string]interface{}{
				"reasoning_tokens": 0,
			},
			"prompt_tokens_details": map[string]interface{}{
				"cached_tokens": 0,
			},
		},
	}
}

func buildStructuredMessage(role, text string) map[string]interface{} {
	if text == "" {
		text = ""
	}
	return map[string]interface{}{
		"role":                        role,
		"content":                     text,
		"response_meta":               blankResponseMeta(),
		"reasoning_content_signature": "",
	}
}

func renderToolCalls(toolCalls []map[string]interface{}) string {
	b, _ := json.Marshal(toolCalls)
	return "Tool calls:\n" + string(b)
}

func summarizeUnresolvedToolCalls(toolCalls []map[string]interface{}) string {
	sb := strings.Builder{}
	sb.WriteString("Previously planned but unexecuted tool calls")
	limit := len(toolCalls)
	if limit > 6 {
		limit = 6
	}
	names := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		name := toolCallName(toolCalls[i])
		if name == "" {
			name = "unknown"
		}
		names = append(names, name)
	}
	if len(names) > 0 {
		sb.WriteString(": ")
		sb.WriteString(strings.Join(names, ", "))
	}
	if len(toolCalls) > 6 {
		sb.WriteString(fmt.Sprintf(" and %d more", len(toolCalls)-6))
	}
	sb.WriteByte('.')
	return sb.String()
}

func toolCallName(call map[string]interface{}) string {
	fn, _ := call["function"].(map[string]interface{})
	if fn == nil {
		return ""
	}
	name, _ := fn["name"].(string)
	return name
}

func normalizeToolArguments(raw interface{}) string {
	switch v := raw.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func normalizeToolCalls(raw []map[string]interface{}) []map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	normalized := make([]map[string]interface{}, 0, len(raw))
	for _, rawCall := range raw {
		fn, _ := rawCall["function"].(map[string]interface{})
		name := ""
		args := ""
		if fn != nil {
			name, _ = fn["name"].(string)
			args = normalizeToolArguments(fn["arguments"])
		}
		if name == "" && args == "" {
			continue
		}
		callType, _ := rawCall["type"].(string)
		if callType == "" {
			callType = "function"
		}
		id, _ := rawCall["id"].(string)
		normalized = append(normalized, map[string]interface{}{
			"id":   id,
			"type": callType,
			"function": map[string]interface{}{
				"name":      name,
				"arguments": args,
			},
		})
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func renderToolResult(message Message, text string) string {
	sb := strings.Builder{}
	sb.WriteString("Tool result")
	if message.Name != "" {
		sb.WriteString(" (")
		sb.WriteString(message.Name)
		sb.WriteByte(')')
	}
	if message.ToolCallID != "" {
		sb.WriteString(" [")
		sb.WriteString(message.ToolCallID)
		sb.WriteByte(']')
	}
	if text != "" {
		sb.WriteString(":\n")
		sb.WriteString(text)
	}
	return sb.String()
}

func joinSections(first, second string) string {
	first = strings.TrimSpace(first)
	second = strings.TrimSpace(second)
	if first == "" {
		return second
	}
	if second == "" {
		return first
	}
	return first + "\n\n" + second
}

func hasMessageRole(messages []Message, role string) bool {
	for _, message := range messages {
		if message.Role == role {
			return true
		}
	}
	return false
}

func truncateString(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

func toJson(v interface{}) string {
	bytes, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(bytes)
}

// QoderAPIError — ошибка из SSE body Qoder (например code 115 = лимит агента).
type QoderAPIError struct {
	Code    string
	Message string
	ResetAt time.Time
}

func (e *QoderAPIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "115" {
		msg := "Лимит агента исчерпан"
		if !e.ResetAt.IsZero() {
			msg += ". Сброс: " + e.ResetAt.Format("2006-01-02 15:04:05 MST")
		}
		return msg
	}
	if e.Message != "" {
		return fmt.Sprintf("Qoder error %s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("Qoder error %s", e.Code)
}

func (e *QoderAPIError) HTTPStatus() int {
	if e != nil && e.Code == "115" {
		return http.StatusTooManyRequests
	}
	return http.StatusBadGateway
}

func (e *QoderAPIError) OpenAIType() string {
	if e != nil && e.Code == "115" {
		return "rate_limit_error"
	}
	return "api_error"
}

func parseQoderStreamError(dataLine string) *QoderAPIError {
	dataLine = strings.TrimSpace(dataLine)
	var wrapper map[string]interface{}
	if err := json.Unmarshal([]byte(dataLine), &wrapper); err != nil {
		return nil
	}
	inner, ok := wrapper["body"].(string)
	if !ok || inner == "" {
		return nil
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(inner), &payload); err != nil {
		return nil
	}
	code := anyToString(payload["code"])
	if code == "" {
		return nil
	}
	// Обычный чат-чанк имеет choices — это не ошибка.
	if _, hasChoices := payload["choices"]; hasChoices {
		return nil
	}

	apiErr := &QoderAPIError{
		Code:    code,
		Message: anyToString(payload["message"]),
	}
	apiErr.ResetAt = parseAgentLimitReset(apiErr.Message)
	return apiErr
}

func parseAgentLimitReset(message string) time.Time {
	message = strings.TrimSpace(message)
	if message == "" {
		return time.Time{}
	}
	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(message), &meta); err != nil {
		return time.Time{}
	}
	raw, ok := meta["agentLimitResetTime"]
	if !ok {
		return time.Time{}
	}
	ms, ok := anyToInt64(raw)
	if !ok || ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).Local()
}

func anyToString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	case json.Number:
		return t.String()
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprintf("%v", v)
	}
}

func anyToInt64(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case int64:
		return t, true
	case json.Number:
		n, err := t.Int64()
		return n, err == nil
	case string:
		var n int64
		_, err := fmt.Sscan(t, &n)
		return n, err == nil
	default:
		return 0, false
	}
}

func writeJSONAPIError(w http.ResponseWriter, apiErr *QoderAPIError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(apiErr.HTTPStatus())
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": apiErr.Error(),
			"type":    apiErr.OpenAIType(),
			"code":    apiErr.Code,
		},
	})
}

func writeStreamAPIError(w http.ResponseWriter, apiErr *QoderAPIError) {
	payload := map[string]interface{}{
		"error": map[string]interface{}{
			"message": apiErr.Error(),
			"type":    apiErr.OpenAIType(),
			"code":    apiErr.Code,
		},
	}
	w.Write([]byte("data: " + toJson(payload) + "\n\n"))
	w.Write([]byte("data: [DONE]\n\n"))
}

func extractDelta(dataLine string) *BridgeDelta {
	dataLine = strings.TrimSpace(dataLine)
	var wrapper map[string]interface{}
	if err := json.Unmarshal([]byte(dataLine), &wrapper); err != nil {
		return &BridgeDelta{}
	}

	inner, ok := wrapper["body"].(string)
	if !ok || inner == "" {
		return &BridgeDelta{}
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(inner), &payload); err != nil {
		return &BridgeDelta{}
	}

	choices, ok := payload["choices"].([]interface{})
	if !ok {
		return &BridgeDelta{}
	}

	result := &BridgeDelta{}
	for _, raw := range choices {
		choice, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		if role, ok := delta["role"].(string); ok && role != "" {
			result.role = role
		}
		if text, ok := delta["content"].(string); ok && text != "" {
			result.content = text
		}
		if toolCalls, ok := delta["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
			result.toolCalls = make([]map[string]interface{}, len(toolCalls))
			for i, tc := range toolCalls {
				if tcMap, ok := tc.(map[string]interface{}); ok {
					result.toolCalls[i] = tcMap
				}
			}
		}
		if result.role != "" || result.content != "" || len(result.toolCalls) > 0 {
			return result
		}
	}
	return result
}

type streamAccumulator struct {
	w                http.ResponseWriter
	reqId            string
	created          int64
	model            string
	toolCallFallback bool
	toolCalls        *toolCallAccumulator
	pendingContent   strings.Builder
	pendingRole      string
	emittedChunk     bool
	streamingText    bool
	contentChunks    int
	toolCallChunks   int
}

func newStreamAccumulator(w http.ResponseWriter, reqId string, created int64, model string, toolsEnabled bool) *streamAccumulator {
	return &streamAccumulator{
		w:                w,
		reqId:            reqId,
		created:          created,
		model:            model,
		toolCallFallback: toolsEnabled,
		toolCalls:        newToolCallAccumulator(),
		pendingRole:      "assistant",
	}
}

func (a *streamAccumulator) accept(delta *BridgeDelta) {
	if role := delta.Role(); role != "" {
		a.pendingRole = role
	}
	if len(delta.ToolCalls()) > 0 {
		a.discardBufferedToolCallText()
		a.toolCalls.append(delta.ToolCalls())
		a.emit("", withToolCallIndices(delta.ToolCalls()))
		a.toolCallChunks++
		return
	}
	if delta.Content() == "" {
		return
	}
	if !a.toolCallFallback || a.streamingText {
		a.streamingText = true
		a.emit(delta.Content(), nil)
		a.contentChunks++
		return
	}
	a.pendingContent.WriteString(delta.Content())
	if isPotentialToolCallText(a.pendingContent.String()) {
		return
	}
	a.streamingText = true
	a.emitBufferedText()
}

func (a *streamAccumulator) flush() {
	if a.pendingContent.Len() == 0 {
		return
	}
	buffered := a.pendingContent.String()
	a.pendingContent.Reset()
	if a.toolCallFallback {
		if parsed := parseToolCallsText(buffered); parsed != nil {
			a.toolCalls.append(parsed)
			a.emit("", withToolCallIndices(parsed))
			a.toolCallChunks++
			return
		}
	}
	a.streamingText = true
	a.emit(buffered, nil)
	a.contentChunks++
}

func (a *streamAccumulator) finishReason() string {
	if a.toolCalls.isEmpty() {
		return "stop"
	}
	return "tool_calls"
}

func (a *streamAccumulator) stats() (int, int) {
	return a.contentChunks, a.toolCallChunks
}

func (a *streamAccumulator) emitBufferedText() {
	if a.pendingContent.Len() == 0 {
		return
	}
	buffered := a.pendingContent.String()
	a.pendingContent.Reset()
	a.emit(buffered, nil)
	a.contentChunks++
}

func (a *streamAccumulator) discardBufferedToolCallText() {
	if a.pendingContent.Len() == 0 {
		return
	}
	buffered := a.pendingContent.String()
	a.pendingContent.Reset()
	if a.toolCallFallback && isPotentialToolCallText(buffered) {
		return
	}
	a.streamingText = true
	a.emit(buffered, nil)
	a.contentChunks++
}

func (a *streamAccumulator) emit(content string, toolCalls []map[string]interface{}) {
	chunk := makeChunk(a.reqId, a.created, a.model)
	delta := chunk["choices"].([]map[string]interface{})[0]["delta"].(map[string]interface{})
	if !a.emittedChunk {
		role := a.pendingRole
		if role == "" {
			role = "assistant"
		}
		delta["role"] = role
	}
	if content != "" {
		delta["content"] = content
	}
	if len(toolCalls) > 0 {
		delta["tool_calls"] = toolCalls
	}
	a.w.Write([]byte("data: " + toJson(chunk) + "\n\n"))
	a.emittedChunk = true
}

func isPotentialToolCallText(text string) bool {
	candidate := strings.TrimLeft(text, " \t\r\n")
	if candidate == "" {
		return true
	}
	return strings.HasPrefix("Tool calls:", candidate) || strings.HasPrefix(candidate, "Tool calls:")
}

func withToolCallIndices(raw []map[string]interface{}) []map[string]interface{} {
	indexed := make([]map[string]interface{}, len(raw))
	for i, call := range raw {
		copyCall := deepCopyMap(call)
		if _, ok := copyCall["index"]; !ok {
			copyCall["index"] = i
		}
		indexed[i] = copyCall
	}
	return indexed
}

type toolCallAccumulator struct {
	calls []map[string]interface{}
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{}
}

func newToolCallPlaceholder() map[string]interface{} {
	return map[string]interface{}{
		"id":   "",
		"type": "function",
		"function": map[string]interface{}{
			"name":      "",
			"arguments": "",
		},
	}
}

func toolCallIndex(deltaCall map[string]interface{}, currentLen int) int {
	if idx, ok := deltaCall["index"].(float64); ok {
		return int(idx)
	}
	if idx, ok := deltaCall["index"].(int); ok {
		return idx
	}
	if currentLen == 0 {
		return 0
	}
	deltaFn, _ := deltaCall["function"].(map[string]interface{})
	if deltaFn != nil {
		if name, ok := deltaFn["name"].(string); ok && name != "" {
			return currentLen
		}
	}
	return currentLen - 1
}

func (a *toolCallAccumulator) append(deltaCalls []map[string]interface{}) {
	for _, deltaCall := range deltaCalls {
		index := toolCallIndex(deltaCall, len(a.calls))
		for len(a.calls) <= index {
			a.calls = append(a.calls, newToolCallPlaceholder())
		}
		existing := a.calls[index]
		if id, ok := deltaCall["id"].(string); ok && id != "" {
			existing["id"] = id
		}
		if typ, ok := deltaCall["type"].(string); ok && typ != "" {
			existing["type"] = typ
		}
		deltaFn, _ := deltaCall["function"].(map[string]interface{})
		if deltaFn == nil {
			continue
		}
		existingFn := existing["function"].(map[string]interface{})
		if name, ok := deltaFn["name"].(string); ok && name != "" {
			existingFn["name"] = name
		}
		if args, ok := deltaFn["arguments"].(string); ok && args != "" {
			prev, _ := existingFn["arguments"].(string)
			existingFn["arguments"] = prev + args
		}
	}
}

func (a *toolCallAccumulator) isEmpty() bool {
	return len(a.calls) == 0
}

func (a *toolCallAccumulator) snapshot() []map[string]interface{} {
	result := make([]map[string]interface{}, len(a.calls))
	for i, call := range a.calls {
		fn := call["function"].(map[string]interface{})
		result[i] = map[string]interface{}{
			"id":    call["id"],
			"type":  call["type"],
			"index": i,
			"function": map[string]interface{}{
				"name":      fn["name"],
				"arguments": fn["arguments"],
			},
		}
	}
	return result
}

func makeChunk(reqId string, created int64, model string) map[string]interface{} {
	return map[string]interface{}{
		"id":      reqId,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"delta": map[string]interface{}{},
			},
		},
	}
}

func parseToolCallsText(text string) []map[string]interface{} {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "Tool calls:") {
		return nil
	}
	payload := strings.TrimSpace(trimmed[len("Tool calls:"):])
	if strings.HasPrefix(payload, "```") && strings.HasSuffix(payload, "```") {
		if newline := strings.Index(payload, "\n"); newline >= 0 {
			payload = strings.TrimSpace(payload[newline+1 : len(payload)-3])
		}
	}
	if !strings.HasPrefix(payload, "[") {
		return nil
	}
	var parsed interface{}
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil
	}
	switch v := parsed.(type) {
	case []interface{}:
		raw := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				raw = append(raw, m)
			}
		}
		return normalizeToolCalls(raw)
	default:
		return nil
	}
}
