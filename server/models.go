package server

import (
	"sort"
	"strings"
	"time"
)

// knownAliases: OpenAI-facing id -> Qoder model key
var knownAliases = map[string]string{
	"lite":              "lite",
	"auto":              "auto",
	"qwen3.8-max":       "qmodel_38max",
	"qwen3.7-max":       "qmodel_latest",
	"qwen3.7-plus":      "qmodel",
	"deepseek-v4-pro":   "dmodel",
	"deepseek-v4-flash": "dfmodel",
	"glm-5.2":           "gm51model",
	"glm-5.1":           "gm51model", // legacy alias
	"kimi-k3":           "kmodel_latest",
	"kimi-k2.6":         "kmodel",
	"kimi-k2.7-code":    "kmodel",
	"minimax-m3":        "mmodel",
	"cantus":            "cmodel",
	"ultimate":          "ultimate",
	"performance":       "performance",
	"efficient":         "efficient",
}

// reverseKnown: Qoder key -> preferred OpenAI id
var reverseKnown map[string]string

func init() {
	reverseKnown = make(map[string]string, len(knownAliases))
	for alias, key := range knownAliases {
		if _, exists := reverseKnown[key]; !exists || preferAlias(alias, reverseKnown[key]) {
			reverseKnown[key] = alias
		}
	}
}

func preferAlias(a, b string) bool {
	if a == b {
		return false
	}
	raw := map[string]bool{"lite": true, "auto": true, "ultimate": true, "performance": true, "efficient": true}
	if raw[b] && !raw[a] {
		return true
	}
	return false
}

type catalogModel struct {
	ID          string
	QoderKey    string
	DisplayName string
	OwnedBy     string
	// Raw per-model configs from Qoder model/list (thinking levels, context sizes).
	ThinkingConfig interface{}
	ContextConfig  interface{}
}

// stripModel1MSuffix removes the 1M context variant suffix from model IDs.
func stripModel1MSuffix(model string) (string, bool) {
	m := strings.TrimSpace(model)
	if m == "" {
		return m, false
	}
	lower := strings.ToLower(m)
	if !strings.HasSuffix(lower, "[1m]") {
		return m, false
	}
	return strings.TrimSpace(m[:len(m)-4]), true
}

func modelRequests1M(model string, context1M bool) bool {
	if context1M {
		return true
	}
	_, use1M := stripModel1MSuffix(model)
	return use1M
}

func defaultModelMapping() map[string]string {
	out := make(map[string]string, len(knownAliases))
	for k, v := range knownAliases {
		out[k] = v
	}
	return out
}

func buildOpenAIModels(items []catalogModel) []map[string]interface{} {
	created := time.Now().Unix()
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	out := make([]map[string]interface{}, 0, len(items))
	seen := map[string]bool{}
	for _, m := range items {
		id := strings.TrimSpace(m.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		owned := m.OwnedBy
		if owned == "" {
			owned = "qoder"
		}
		entry := map[string]interface{}{
			"id":       id,
			"object":   "model",
			"created":  created,
			"owned_by": owned,
		}
		if m.DisplayName != "" {
			entry["name"] = m.DisplayName
			entry["display_name"] = m.DisplayName
		}
		if m.QoderKey != "" && m.QoderKey != id {
			entry["root"] = m.QoderKey
		}
		if m.ThinkingConfig != nil {
			entry["thinking_config"] = m.ThinkingConfig
		}
		if m.ContextConfig != nil {
			entry["context_config"] = m.ContextConfig
		}
		out = append(out, entry)
	}
	return out
}

func modelsFromMapping(mapping map[string]string) []catalogModel {
	items := make([]catalogModel, 0, len(mapping))
	for id, key := range mapping {
		items = append(items, catalogModel{
			ID:          id,
			QoderKey:    key,
			DisplayName: id,
			OwnedBy:     "qoder",
		})
	}
	return items
}

func parseQoderModelList(raw map[string]interface{}) []catalogModel {
	if raw == nil {
		return nil
	}
	seenKeys := map[string]catalogModel{}

	// Prefer chat/assistant catalogs — skip quest/qwork/nap noise for OpenAI discovery.
	preferred := []string{"chat", "assistant"}
	foundPreferred := false
	for _, cat := range preferred {
		if arr, ok := raw[cat].([]interface{}); ok && len(arr) > 0 {
			foundPreferred = true
			collectModels(arr, seenKeys)
		}
	}
	if !foundPreferred {
		collectModels(raw, seenKeys)
	}

	items := make([]catalogModel, 0, len(seenKeys)*2)
	for _, m := range seenKeys {
		items = append(items, m)
		if m.QoderKey != m.ID {
			items = append(items, catalogModel{
				ID:          m.QoderKey,
				QoderKey:    m.QoderKey,
				DisplayName: m.DisplayName,
				OwnedBy:     "qoder",
			})
		}
	}
	return items
}

func collectModels(v interface{}, seenKeys map[string]catalogModel) {
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case map[string]interface{}:
			key := firstString(t, "key", "model_key", "modelKey", "id")
			if key != "" && looksLikeModelKey(key, t) {
				display := firstString(t, "display_name", "displayName", "name", "title")
				if display == "" {
					display = key
				}
				seenKeys[key] = catalogModel{
					ID:             openAIIDForKey(key, display),
					QoderKey:       key,
					DisplayName:    display,
					OwnedBy:        "qoder",
					ThinkingConfig: t["thinking_config"],
					ContextConfig:  t["context_config"],
				}
			}
			for _, child := range t {
				walk(child)
			}
		case []interface{}:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(v)
}

func looksLikeModelKey(key string, m map[string]interface{}) bool {
	key = strings.TrimSpace(key)
	if key == "" || strings.Contains(key, " ") {
		return false
	}
	// Strong signals from Qoder model objects
	if _, ok := m["display_name"]; ok {
		return true
	}
	if _, ok := m["displayName"]; ok {
		return true
	}
	if _, ok := m["price_factor"]; ok {
		return true
	}
	if _, ok := m["is_free"]; ok {
		return true
	}
	if _, ok := m["max_input_tokens"]; ok {
		return true
	}
	if _, ok := m["context_config"]; ok {
		return true
	}
	// Known keys / short identifiers
	if _, ok := reverseKnown[key]; ok {
		return true
	}
	if strings.HasSuffix(key, "model") || strings.Contains(key, "model") {
		return true
	}
	switch key {
	case "lite", "auto", "ultimate", "performance", "efficient":
		return true
	}
	return false
}

func openAIIDForKey(key, display string) string {
	if alias, ok := reverseKnown[key]; ok {
		return alias
	}
	d := strings.TrimSpace(display)
	if d != "" && d != key && !strings.Contains(d, " ") {
		return strings.ToLower(d)
	}
	return key
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if strings.TrimSpace(t) != "" {
					return strings.TrimSpace(t)
				}
			}
		}
	}
	return ""
}

func mergeMappingWithCatalog(base map[string]string, items []catalogModel) map[string]string {
	out := make(map[string]string, len(base)+len(items))
	for k, v := range base {
		out[k] = v
	}
	for _, m := range items {
		if m.QoderKey == "" {
			continue
		}
		out[m.ID] = m.QoderKey
		out[m.QoderKey] = m.QoderKey
	}
	return out
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
