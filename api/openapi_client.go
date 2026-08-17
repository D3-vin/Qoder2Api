package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"qoder2api/httputil"
)

const openAPIBase = "https://openapi.qoder.sh"

// OpenApiError carries HTTP status so callers can detect auth failures.
type OpenApiError struct {
	Status int
	Body   string
}

func (e *OpenApiError) Error() string {
	return fmt.Sprintf("openapi HTTP %d: %s", e.Status, truncate(e.Body, 200))
}

func IsAuthError(err error) bool {
	apiErr, ok := err.(*OpenApiError)
	return ok && (apiErr.Status == 401 || apiErr.Status == 403)
}

type OpenApiClient struct {
	client *http.Client
}

func NewOpenApiClient() *OpenApiClient {
	return &OpenApiClient{client: httputil.NewClient(20 * time.Second)}
}

// openapiHeaders mirrors v3pro qoder_http.openapi_headers.
func openapiHeaders(bearer string) map[string]string {
	h := map[string]string{
		"accept":           "application/json",
		"user-agent":       "qoder/1.1.20",
		"cosy-version":     "1.1.20",
		"cosy-clienttype":  "5",
		"cosy-machineos":   "x86_64_win32",
		"accept-encoding":  "identity",
		"traceparent":      traceparent(),
	}
	if bearer != "" {
		h["authorization"] = "Bearer " + bearer
	}
	return h
}

func traceparent() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return fmt.Sprintf("00-%s-%s-01", hex.EncodeToString(b[:16]), hex.EncodeToString(b[16:]))
}

func (c *OpenApiClient) do(method, path, bearer string, body []byte) (map[string]interface{}, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, openAPIBase+path, reader)
	if err != nil {
		return nil, err
	}
	for k, v := range openapiHeaders(bearer) {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, &OpenApiError{Status: resp.StatusCode, Body: string(raw)}
	}

	var result map[string]interface{}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("openapi parse %s: %w", path, err)
	}
	return result, nil
}

// ExchangePAT swaps a PAT for a jt- token (openapi jobToken exchange).
func (c *OpenApiClient) ExchangePAT(pat string) (string, error) {
	body, _ := json.Marshal(map[string]string{"personal_token": pat})
	result, err := c.do("POST", "/api/v1/jobToken/exchange", "", body)
	if err != nil {
		return "", err
	}
	if token, ok := result["token"].(string); ok && token != "" {
		return token, nil
	}
	if data, ok := result["data"].(map[string]interface{}); ok {
		if token, ok := data["token"].(string); ok && token != "" {
			return token, nil
		}
	}
	return "", fmt.Errorf("jobToken/exchange: no token in response")
}

// QuotaUsage returns raw /api/v2/quota/usage payload (userQuota.total/used/remaining).
func (c *OpenApiClient) QuotaUsage(token string) (map[string]interface{}, error) {
	return c.do("GET", "/api/v2/quota/usage", token, nil)
}

// UserPlan returns raw /api/v2/user/plan payload.
func (c *OpenApiClient) UserPlan(token string) (map[string]interface{}, error) {
	return c.do("GET", "/api/v2/user/plan", token, nil)
}

// PromoEligibility returns activity list from /api/v2/activity/claim/eligibility.
func (c *OpenApiClient) PromoEligibility(jt string) ([]map[string]interface{}, error) {
	result, err := c.do("GET", "/api/v2/activity/claim/eligibility", jt, nil)
	if err != nil {
		return nil, err
	}
	arr, ok := result["data"].([]interface{})
	if !ok {
		return nil, nil
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out, nil
}
