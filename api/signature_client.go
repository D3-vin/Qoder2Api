package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"

	"qoder2api/auth"
	"qoder2api/encoding"
	"qoder2api/httputil"
	"qoder2api/logx"
)

type SignatureApiClient struct {
	client       *http.Client
	machineId    string
	machineToken string
	machineType  string
}

func NewSignatureApiClient(machineId, machineToken, machineType string) *SignatureApiClient {
	return &SignatureApiClient{
		client:       httputil.NewClient(15 * time.Second),
		machineId:    machineId,
		machineToken: machineToken,
		machineType:  machineType,
	}
}

func (c *SignatureApiClient) ExchangeJobToken(personalToken string) (map[string]interface{}, error) {
	inner := map[string]interface{}{
		"personalToken":      personalToken,
		"securityOauthToken": "",
		"refreshToken":       "",
		"needRefresh":        false,
		"authInfo":           map[string]interface{}{},
	}

	outer := map[string]interface{}{
		"payload":       toJson(inner),
		"encodeVersion": "1",
	}

	return c.postEncoded("https://center.qoder.sh/algo/api/v3/user/jobToken?Encode=1", outer)
}

// RefreshJobToken asks the gateway to renew stale securityOauthToken/refreshToken.
func (c *SignatureApiClient) RefreshJobToken(personalToken, securityOauthToken, refreshToken string) (map[string]interface{}, error) {
	inner := map[string]interface{}{
		"personalToken":      personalToken,
		"securityOauthToken": securityOauthToken,
		"refreshToken":       refreshToken,
		"needRefresh":        true,
		"authInfo":           map[string]interface{}{},
	}

	outer := map[string]interface{}{
		"payload":       toJson(inner),
		"encodeVersion": "1",
	}

	return c.postEncoded("https://center.qoder.sh/algo/api/v3/user/jobToken?Encode=1", outer)
}

func (c *SignatureApiClient) UserStatus(userId string) (map[string]interface{}, error) {
	inner := map[string]interface{}{
		"userId":             userId,
		"personalToken":      "",
		"securityOauthToken": "",
		"refreshToken":       "",
		"needRefresh":        false,
		"authInfo":           map[string]interface{}{},
	}

	outer := map[string]interface{}{
		"payload":       toJson(inner),
		"encodeVersion": "1",
	}

	return c.postEncoded("https://center.qoder.sh/algo/api/v3/user/status?Encode=1", outer)
}

func (c *SignatureApiClient) Heartbeat() (map[string]interface{}, error) {
	hb := map[string]interface{}{
		"event_time":  time.Now().UnixMilli(),
		"event_type":  "cosy_heartbeat",
		"mid":         c.machineId,
		"os_arch":     getOsArch(),
		"os_version":  getOsVersion(),
		"ide_type":    "qodercli",
		"ide_version": CosyVersionCLI,
		// "ide_version": "0.1.43",
		"extra_info": map[string]interface{}{},
	}

	return c.postEncoded("https://center.qoder.sh/algo/api/v1/heartbeat?Encode=1", hb)
}

func (c *SignatureApiClient) postEncoded(url string, obj interface{}) (map[string]interface{}, error) {
	date := auth.CurrentDate()
	sig := auth.Sign(date)

	plain, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}

	body := encoding.Encode(plain)

	// Debug output
	logx.Debugf("[DEBUG] URL: %s\n", url)
	logx.Debugf("[DEBUG] Date: %s\n", date)
	logx.Debugf("[DEBUG] Signature input: %s&%s&%s\n", auth.APPCODE, auth.SECRET, date)
	logx.Debugf("[DEBUG] Signature: %s\n", sig)

	bodyPreview := body
	if len(body) > 100 {
		bodyPreview = body[:100]
	}
	logx.Debugf("[DEBUG] Body (encoded): %s\n", bodyPreview)
	logx.Debugf("[DEBUG] MachineToken: %s\n", c.machineToken)
	logx.Debugf("[DEBUG] MachineType: %s\n", c.machineType)
	logx.Debugf("[DEBUG] MachineId: %s\n", c.machineId)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(body)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("cosy-machinetoken", c.machineToken)
	req.Header.Set("cosy-machinetype", c.machineType)
	req.Header.Set("login-version", "v2")
	req.Header.Set("appcode", auth.APPCODE)
	req.Header.Set("accept", "application/json")
	req.Header.Set("accept-encoding", "identity")
	req.Header.Set("cosy-version", CosyVersionCLI)
	// req.Header.Set("cosy-version", "0.1.43")
	req.Header.Set("cosy-clienttype", "5")
	req.Header.Set("date", date)
	req.Header.Set("signature", sig)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cosy-machineid", c.machineId)
	req.Header.Set("user-agent", "Go-http-client/2.0")

	resp, err := c.client.Do(req)
	if err != nil {
		logx.Debugf("[DEBUG] Do failed: %v\n", err)
		return nil, err
	}
	defer resp.Body.Close()
	logx.Debugf("[DEBUG] response status=%d url=%s\n", resp.StatusCode, url)

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d at %s body=%s", resp.StatusCode, url, string(bodyBytes))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return result, nil
}

func toJson(v interface{}) string {
	bytes, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func getOsArch() string {
	arch := runtime.GOARCH
	if arch == "amd64" {
		return "windows_amd64"
	}
	return arch
}

func getOsVersion() string {
	return runtime.GOOS + " " + getOsVersionDetail()
}

func getOsVersionDetail() string {
	// Simplified - in production use proper OS detection
	return "unknown"
}
