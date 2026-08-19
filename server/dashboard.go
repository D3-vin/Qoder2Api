// dashboard.go — embedded HTML dashboard (GitHub-dark style, from example/) + /api routes.
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func (p *BridgePool) Start(port int) error {
	p.port = port
	host := getSetting("QODER_HOST")
	if host == "" {
		host = "127.0.0.1"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", p.handleDashboard)
	mux.HandleFunc("/v1/chat/completions", p.dispatchChat)
	mux.HandleFunc("/v1/messages", p.dispatchMessages)
	mux.HandleFunc("/v1/models", p.handleModels)
	mux.HandleFunc("/v1/usage", p.handleUsage)
	mux.HandleFunc("/api/status", p.apiStatus)
	mux.HandleFunc("/api/accounts", p.apiAccounts)
	mux.HandleFunc("/api/accounts/add", p.apiAccountsAdd)
	mux.HandleFunc("/api/accounts/remove", p.apiAccountsRemove)
	mux.HandleFunc("/api/accounts/select", p.apiAccountsSelect)
	mux.HandleFunc("/api/quota/refresh", p.apiQuotaRefresh)
	mux.HandleFunc("/api/promo", p.apiPromo)
	mux.HandleFunc("/api/settings", p.apiSettings)

	addr := fmt.Sprintf("%s:%d", host, port)
	fmt.Printf("[pool] listening http://%s/ (dashboard)\n", addr)
	fmt.Printf("[pool] listening http://%s/v1/chat/completions  (OpenAI)\n", addr)
	fmt.Printf("[pool] listening http://%s/v1/messages          (Anthropic)\n", addr)
	fmt.Printf("[pool] listening http://%s/v1/models\n", addr)
	fmt.Printf("[pool] listening http://%s/v1/usage\n", addr)
	return http.ListenAndServe(addr, mux)
}

func (p *BridgePool) handleModels(w http.ResponseWriter, r *http.Request) {
	p.activeAccount().bridge.handleModels(w, r)
}

func (p *BridgePool) handleUsage(w http.ResponseWriter, r *http.Request) {
	p.activeAccount().bridge.handleUsage(w, r)
}

func (p *BridgePool) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

// --- /api handlers ---

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
}

func (p *BridgePool) apiStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	p.mu.Lock()
	acc := p.accounts[p.active]
	status := map[string]interface{}{
		"running":       true,
		"port":          p.port,
		"accounts":      len(p.accounts),
		"active_pat":    maskPAT(acc.pat),
		"active_user":   acc.bridge.sess.Identity.Name,
		"default_model": p.cfg.DefaultModel,
		"context":       p.cfg.ModelSettings[p.cfg.DefaultModel].Context,
		"thinking":      p.cfg.ModelSettings[p.cfg.DefaultModel].Thinking,
	}
	p.mu.Unlock()
	writeJSON(w, status)
}

func (p *BridgePool) accountsJSON() []map[string]interface{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	out := make([]map[string]interface{}, 0, len(p.accounts))
	for i, acc := range p.accounts {
		entry := map[string]interface{}{
			"index":  i,
			"pat":    maskPAT(acc.pat),
			"active": i == p.active,
			"user":   acc.bridge.sess.Identity.Name,
			"uid":    acc.bridge.sess.Identity.Uid,
			"quota": map[string]interface{}{
				"source":     acc.quotaSrc,
				"fetched_at": acc.quotaAt.Format(time.RFC3339),
				"data":       acc.quotaRaw,
			},
			"plan":        acc.planRaw,
			"free_quotas": acc.freeQuotas,
		}
		if !acc.exhaustedUntil.IsZero() && acc.exhaustedUntil.After(now) {
			entry["exhausted_until"] = acc.exhaustedUntil.Format(time.RFC3339)
		}
		out = append(out, entry)
	}
	return out
}

func (p *BridgePool) apiAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, map[string]interface{}{"accounts": p.accountsJSON()})
}

func decodePATBody(r *http.Request) (string, error) {
	var body struct {
		PAT string `json:"pat"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.PAT, nil
}

func decodeIndexBody(r *http.Request) (int, error) {
	var body struct {
		Index int `json:"index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return -1, err
	}
	return body.Index, nil
}

func (p *BridgePool) apiAccountsAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	pat, err := decodePATBody(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	if err := p.AddPAT(pat); err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (p *BridgePool) apiAccountsRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	idx, err := decodeIndexBody(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	if err := p.RemoveIndex(idx); err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (p *BridgePool) apiAccountsSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	idx, err := decodeIndexBody(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	if err := p.SelectIndex(idx); err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (p *BridgePool) apiQuotaRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	p.RefreshQuotas()
	writeJSON(w, map[string]interface{}{"ok": true, "accounts": p.accountsJSON()})
}

func (p *BridgePool) apiPromo(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	list, err := p.PromoList()
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err)
		return
	}
	acc := p.activeAccount()
	p.mu.Lock()
	free := acc.freeQuotas
	p.mu.Unlock()
	for _, act := range list {
		id, _ := act["activityId"].(string)
		if fq, ok := free[id]; ok {
			act["free_quota"] = fq
		}
	}
	writeJSON(w, map[string]interface{}{"activities": list})
}

func (p *BridgePool) apiSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		DefaultModel string `json:"default_model"`
		Model        string `json:"model"`
		Context      string `json:"context"`
		Thinking     string `json:"thinking"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, err)
		return
	}
	if body.DefaultModel != "" {
		p.SetDefaultModel(body.DefaultModel)
	}
	if body.Model != "" {
		p.SetRuntime(body.Model, body.Context, body.Thinking)
	} else if body.Context != "" || body.Thinking != "" {
		p.SetRuntime(p.cfg.DefaultModel, body.Context, body.Thinking)
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}
