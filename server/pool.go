package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"qoder2api/api"
)

// limitErrorMarker is the text QoderAPIError.Error() produces for code 115.
const limitErrorMarker = "Лимит агента исчерпан"

func isLimitAPIError(err error) bool {
	apiErr, ok := err.(*QoderAPIError)
	return ok && apiErr.Code == "115"
}

// failoverWriter buffers handler output until the first real content arrives.
// While buffered, an agent-limit error lets the pool switch PAT and retry
// without the client seeing anything; after commit writes pass through.
type failoverWriter struct {
	target     http.ResponseWriter
	mode       string // "openai_stream" | "anthropic_stream" | "buffered"
	buf        bytes.Buffer
	status     int
	decided    bool
	limitReset time.Time
}

func newFailoverWriter(w http.ResponseWriter, mode string) *failoverWriter {
	return &failoverWriter{target: w, mode: mode}
}

func (f *failoverWriter) Header() http.Header { return f.target.Header() }

func (f *failoverWriter) WriteHeader(code int) {
	if f.decided {
		f.target.WriteHeader(code)
		return
	}
	if f.status == 0 {
		f.status = code
	}
}

func (f *failoverWriter) Write(p []byte) (int, error) {
	if f.decided {
		return f.target.Write(p)
	}
	f.buf.Write(p)
	f.sniff()
	return len(p), nil
}

func (f *failoverWriter) Flush() {
	if f.decided {
		if flusher, ok := f.target.(http.Flusher); ok {
			flusher.Flush()
		}
	}
}

// sniff commits the buffer once the first success content is observable.
func (f *failoverWriter) sniff() {
	switch f.mode {
	case "openai_stream":
		// Any success chunk (raw Qoder wrapper or re-emitted OpenAI chunk)
		// carries "choices"; bridge error SSE events never do.
		if strings.Contains(f.buf.String(), "choices") {
			f.commit()
		}
	case "anthropic_stream":
		if bytes.Contains(f.buf.Bytes(), []byte("content_block_delta")) {
			f.commit()
		}
	}
}

func (f *failoverWriter) commit() {
	if f.decided {
		return
	}
	f.decided = true
	if f.status == 0 {
		f.status = http.StatusOK
	}
	f.target.WriteHeader(f.status)
	if f.buf.Len() > 0 {
		f.target.Write(f.buf.Bytes())
		f.buf.Reset()
	}
	if flusher, ok := f.target.(http.Flusher); ok {
		flusher.Flush()
	}
}

// bufferedLimitError detects an agent-limit response in buffered output.
func (f *failoverWriter) bufferedLimitError() bool {
	if f.status == http.StatusTooManyRequests {
		return true
	}
	data := f.buf.String()
	if strings.Contains(data, limitErrorMarker) {
		return true
	}
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		if apiErr := parseQoderStreamError(strings.TrimSpace(line[5:])); apiErr != nil && isLimitAPIError(apiErr) {
			f.limitReset = apiErr.ResetAt
			return true
		}
	}
	return false
}

// finalize flushes buffered output after the handler returns.
// Returns true when the response was an agent-limit error and the caller may retry.
func (f *failoverWriter) finalize() bool {
	if f.decided {
		return false
	}
	if f.bufferedLimitError() {
		return true
	}
	f.commit()
	return false
}

type accountEntry struct {
	pat            string
	bridge         *OpenAiBridge
	exhaustedUntil time.Time
	jt             string
	quotaRaw       map[string]interface{}
	planRaw        map[string]interface{}
	freeQuotas     map[string]map[string]interface{} // activityId -> {remaining, limit, used}
	quotaSrc       string
	quotaAt        time.Time
}

type BridgePool struct {
	mu       sync.Mutex
	cfg      *Config
	accounts []*accountEntry
	active   int
	openapi  *api.OpenApiClient
	port     int
}

func NewBridgePool(cfg *Config) (*BridgePool, error) {
	pats := cfg.Pats
	if len(pats) == 0 {
		pats = PatsFromEnv()
	}
	if len(pats) == 0 {
		return nil, fmt.Errorf("no PAT configured: set QODER_PAT/QODER_PAT_LIST or add one via dashboard")
	}

	pool := &BridgePool{cfg: cfg, openapi: api.NewOpenApiClient()}
	for _, pat := range pats {
		bridge, err := NewOpenAiBridge(pat)
		if err != nil {
			fmt.Printf("[pool] PAT %s init failed: %v\n", maskPAT(pat), err)
			continue
		}
		pool.accounts = append(pool.accounts, &accountEntry{pat: pat, bridge: bridge})
	}
	if len(pool.accounts) == 0 {
		return nil, fmt.Errorf("all configured PATs failed to initialize")
	}

	pool.active = pool.indexOfPATLocked(cfg.ActivePAT)
	if pool.active < 0 {
		pool.active = 0
	}
	cfg.Pats = pool.patsLocked()
	cfg.ActivePAT = pool.accounts[pool.active].pat
	if err := cfg.Save(); err != nil {
		fmt.Printf("[pool] config save failed: %v\n", err)
	}
	go pool.RefreshQuotas()
	return pool, nil
}

func (p *BridgePool) indexOfPATLocked(pat string) int {
	for i, acc := range p.accounts {
		if acc.pat == pat {
			return i
		}
	}
	return -1
}

func (p *BridgePool) patsLocked() []string {
	out := make([]string, 0, len(p.accounts))
	for _, acc := range p.accounts {
		out = append(out, acc.pat)
	}
	return out
}

func maskPAT(pat string) string {
	if len(pat) <= 10 {
		return "***"
	}
	return pat[:6] + "…" + pat[len(pat)-4:]
}

// pick returns the (attempt+1)-th non-exhausted account starting from active.
func (p *BridgePool) pick(attempt int) *accountEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	n := len(p.accounts)
	for step := 0; step < n; step++ {
		acc := p.accounts[(p.active+step)%n]
		if acc.exhaustedUntil.After(now) {
			continue
		}
		if attempt == 0 {
			return acc
		}
		attempt--
	}
	return nil
}

func (p *BridgePool) markExhausted(acc *accountEntry, resetAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	until := resetAt
	if until.IsZero() || until.Before(time.Now()) {
		until = time.Now().Add(10 * time.Minute)
	}
	acc.exhaustedUntil = until
	fmt.Printf("[pool] %s exhausted until %s\n", maskPAT(acc.pat), until.Format(time.RFC3339))
}

func (p *BridgePool) dispatchChat(w http.ResponseWriter, r *http.Request) {
	p.dispatch(w, r, false)
}

func (p *BridgePool) dispatchMessages(w http.ResponseWriter, r *http.Request) {
	p.dispatch(w, r, true)
}

func (p *BridgePool) dispatch(w http.ResponseWriter, r *http.Request, anthropic bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	body = p.injectDefaults(body)

	for i := 0; i < len(p.accounts); i++ {
		acc := p.pick(i)
		if acc == nil {
			break
		}
		mode := "buffered"
		if bodyHasStream(body) {
			mode = "openai_stream"
			if anthropic {
				mode = "anthropic_stream"
			}
		}
		fw := newFailoverWriter(w, mode)
		r2 := r.Clone(r.Context())
		r2.Body = io.NopCloser(bytes.NewReader(body))
		r2.ContentLength = int64(len(body))

		if anthropic {
			acc.bridge.handleMessages(fw, r2)
		} else {
			acc.bridge.handleChat(fw, r2)
		}

		if fw.finalize() {
			p.markExhausted(acc, fw.limitReset)
			fmt.Printf("[pool] %s hit agent limit, failing over\n", maskPAT(acc.pat))
			continue
		}
		return
	}
	p.writePoolExhausted(w, anthropic)
}

func (p *BridgePool) writePoolExhausted(w http.ResponseWriter, anthropic bool) {
	p.mu.Lock()
	var earliest time.Time
	for _, acc := range p.accounts {
		if earliest.IsZero() || acc.exhaustedUntil.Before(earliest) {
			earliest = acc.exhaustedUntil
		}
	}
	p.mu.Unlock()

	msg := "all PATs exhausted"
	if !earliest.IsZero() {
		msg += "; earliest reset: " + earliest.Format(time.RFC3339)
	}
	if anthropic {
		writeAnthropicJSONError(w, http.StatusTooManyRequests, "rate_limit_error", msg)
		return
	}
	writeJSONAPIError(w, &QoderAPIError{Code: "115", Message: msg, ResetAt: earliest})
}

// injectDefaults fills model/context/thinking from dashboard settings
// when the client did not specify them explicitly.
func (p *BridgePool) injectDefaults(body []byte) []byte {
	var obj map[string]interface{}
	if json.Unmarshal(body, &obj) != nil {
		return body
	}

	// Read DefaultModel and ModelSettings[model] in one locked section:
	// SetRuntime writes ModelSettings concurrently with dispatch.
	p.mu.Lock()
	if m, _ := obj["model"].(string); strings.TrimSpace(m) == "" && p.cfg.DefaultModel != "" {
		obj["model"] = p.cfg.DefaultModel
	}
	model, _ := obj["model"].(string)
	ms := p.cfg.ModelSettings[model]
	p.mu.Unlock()

	if _, ok := obj["context_size"]; !ok && ms.Context != "" {
		obj["context_size"] = ms.Context
	}
	if _, ok := obj["thinking"]; !ok && ms.Thinking != "" {
		obj["thinking"] = ms.Thinking
	}
	if out, err := json.Marshal(obj); err == nil {
		return out
	}
	return body
}

func bodyHasStream(body []byte) bool {
	var obj map[string]interface{}
	if json.Unmarshal(body, &obj) != nil {
		return false
	}
	stream, _ := obj["stream"].(bool)
	return stream
}

func (p *BridgePool) activeAccount() *accountEntry {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.accounts[p.active]
}

// --- account management (dashboard) ---

func (p *BridgePool) AddPAT(pat string) error {
	pat = strings.TrimSpace(pat)
	if pat == "" {
		return fmt.Errorf("empty PAT")
	}
	p.mu.Lock()
	exists := p.indexOfPATLocked(pat) >= 0
	p.mu.Unlock()
	if exists {
		return fmt.Errorf("PAT already in pool")
	}

	bridge, err := NewOpenAiBridge(pat)
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.indexOfPATLocked(pat) >= 0 {
		return fmt.Errorf("PAT already in pool")
	}
	p.accounts = append(p.accounts, &accountEntry{pat: pat, bridge: bridge})
	p.cfg.Pats = p.patsLocked()
	if err := p.cfg.Save(); err != nil {
		fmt.Printf("[pool] config save failed: %v\n", err)
	}
	return nil
}

func (p *BridgePool) RemoveIndex(index int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.accounts) {
		return fmt.Errorf("account index out of range")
	}
	if len(p.accounts) == 1 {
		return fmt.Errorf("cannot remove the last PAT")
	}
	p.accounts = append(p.accounts[:index], p.accounts[index+1:]...)
	if p.active >= len(p.accounts) {
		p.active = 0
	} else if index < p.active {
		p.active--
	} else if index == p.active {
		p.active = 0
	}
	p.cfg.Pats = p.patsLocked()
	p.cfg.ActivePAT = p.accounts[p.active].pat
	if err := p.cfg.Save(); err != nil {
		fmt.Printf("[pool] config save failed: %v\n", err)
	}
	return nil
}

func (p *BridgePool) SelectIndex(index int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.accounts) {
		return fmt.Errorf("account index out of range")
	}
	p.active = index
	p.cfg.ActivePAT = p.accounts[index].pat
	if err := p.cfg.Save(); err != nil {
		fmt.Printf("[pool] config save failed: %v\n", err)
	}
	return nil
}

func (p *BridgePool) SetDefaultModel(model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfg.DefaultModel = strings.TrimSpace(model)
	if err := p.cfg.Save(); err != nil {
		fmt.Printf("[pool] config save failed: %v\n", err)
	}
}

// SetRuntime persists per-model context/thinking settings; dispatch injects them per request.
func (p *BridgePool) SetRuntime(model, contextSize, thinking string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	model = strings.TrimSpace(model)
	if model == "" {
		return
	}
	if p.cfg.ModelSettings == nil {
		p.cfg.ModelSettings = map[string]ModelSetting{}
	}
	ms := p.cfg.ModelSettings[model]
	ms.Context = contextSize
	ms.Thinking = thinking
	p.cfg.ModelSettings[model] = ms
	if err := p.cfg.Save(); err != nil {
		fmt.Printf("[pool] config save failed: %v\n", err)
	}
}

// --- quota / promo (dashboard) ---

// ensureJT returns the account's jt- token, exchanging the PAT on first use.
func (p *BridgePool) ensureJT(acc *accountEntry) string {
	p.mu.Lock()
	jt := acc.jt
	p.mu.Unlock()
	if jt != "" {
		return jt
	}
	token, err := p.openapi.ExchangePAT(acc.pat)
	if err != nil {
		fmt.Printf("[pool] jt exchange failed for %s: %v\n", maskPAT(acc.pat), err)
		return ""
	}
	p.mu.Lock()
	acc.jt = token
	p.mu.Unlock()
	return token
}

func (p *BridgePool) refreshAccountQuota(acc *accountEntry) {
	var raw map[string]interface{}
	src := "error"

	if jt := p.ensureJT(acc); jt != "" {
		if usage, err := p.openapi.QuotaUsage(jt); err == nil {
			raw, src = usage, "openapi/quota/usage"
		} else {
			fmt.Printf("[pool] quota/usage failed for %s: %v\n", maskPAT(acc.pat), err)
		}
		if plan, err := p.openapi.UserPlan(jt); err == nil {
			p.mu.Lock()
			acc.planRaw = plan
			p.mu.Unlock()
		}
	}
	if raw == nil {
		// Fallback: signed algo user/status (shape differs, best effort).
		sig := api.NewSignatureApiClient(acc.bridge.sess.MachineId, acc.bridge.sess.MachineToken, acc.bridge.sess.MachineType)
		if status, err := sig.UserStatus(acc.bridge.sess.Identity.Uid); err == nil {
			raw, src = status, "algo/user/status"
		} else {
			raw = map[string]interface{}{"error": err.Error()}
		}
	}

	// Promo free quotas (remaining/limit per claimed activity).
	free := map[string]map[string]interface{}{}
	if acts, err := acc.bridge.bearerClient.ListActivities(); err == nil {
		free = parseFreeQuotas(acts)
	} else {
		fmt.Printf("[pool] activity failed for %s: %v\n", maskPAT(acc.pat), err)
	}

	p.mu.Lock()
	acc.quotaRaw = raw
	acc.quotaSrc = src
	acc.quotaAt = time.Now()
	acc.freeQuotas = free
	p.mu.Unlock()
}

// parseFreeQuotas extracts MODEL_FREE_QUOTA entries from /algo/api/v2/activity.
func parseFreeQuotas(raw map[string]interface{}) map[string]map[string]interface{} {
	out := map[string]map[string]interface{}{}
	data, _ := raw["data"].(map[string]interface{})
	arr, _ := data["activities"].([]interface{})
	for _, item := range arr {
		act, ok := item.(map[string]interface{})
		if !ok || act["type"] != "MODEL_FREE_QUOTA" {
			continue
		}
		id, _ := act["activityId"].(string)
		if id == "" {
			continue
		}
		out[id] = map[string]interface{}{
			"remaining":  act["remaining"],
			"limit":      act["limit"],
			"used":       act["used"],
			"model_name": act["modelName"],
		}
	}
	return out
}

func (p *BridgePool) RefreshQuotas() {
	p.mu.Lock()
	accs := append([]*accountEntry{}, p.accounts...)
	p.mu.Unlock()
	for _, acc := range accs {
		p.refreshAccountQuota(acc)
	}
}

func (p *BridgePool) PromoList() ([]map[string]interface{}, error) {
	acc := p.activeAccount()
	jt := p.ensureJT(acc)
	if jt == "" {
		return nil, fmt.Errorf("no jt token for %s", maskPAT(acc.pat))
	}
	return p.openapi.PromoEligibility(jt)
}
