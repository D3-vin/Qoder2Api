package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"qoder2api/encoding"
	"qoder2api/httputil"
)

type Session struct {
	UserId              string `json:"id"`
	Name                string `json:"name"`
	SecurityOauthToken   string `json:"securityOauthToken"`
	RefreshToken        string `json:"refreshToken"`
	ExpireTime          int64  `json:"expireTime"`
	Email               string `json:"email"`
	Plan                string `json:"plan"`
	Raw                 string `json:"-"`
}

func ExchangeJobToken(personalToken, machineId, machineToken, machineType string) (*Session, error) {
	date := CurrentDate()
	sig := Sign(date)

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

	plain, err := json.Marshal(outer)
	if err != nil {
		return nil, err
	}

	body := encoding.Encode(plain)

	client := httputil.NewClient(15 * time.Second)

	req, err := http.NewRequest("POST", "https://center.qoder.sh/algo/api/v3/user/jobToken?Encode=1", bytes.NewBuffer([]byte(body)))
	if err != nil {
		return nil, err
	}

	req.Header.Set("cosy-machinetoken", machineToken)
	req.Header.Set("cosy-machinetype", machineType)
	req.Header.Set("login-version", "v2")
	req.Header.Set("appcode", APPCODE)
	req.Header.Set("accept", "application/json")
	req.Header.Set("accept-encoding", "identity")
	req.Header.Set("cosy-version", CosyVersionCLI)
	// req.Header.Set("cosy-version", "0.1.43")
	req.Header.Set("cosy-clienttype", "5")
	req.Header.Set("date", date)
	req.Header.Set("signature", sig)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cosy-machineid", machineId)
	req.Header.Set("user-agent", "Go-http-client/2.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jobToken HTTP %d body=%s", resp.StatusCode, string(bodyBytes))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &Session{
		UserId:            getString(result, "id"),
		Name:              getString(result, "name"),
		SecurityOauthToken: getString(result, "securityOauthToken"),
		RefreshToken:      getString(result, "refreshToken"),
		ExpireTime:        getInt64(result, "expireTime"),
		Email:             getString(result, "email"),
		Plan:              getString(result, "plan"),
		Raw:               string(respBody),
	}, nil
}

func toJson(v interface{}) string {
	bytes, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

func getInt64(m map[string]interface{}, key string) int64 {
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case int:
			return int64(v)
		}
	}
	return 0
}
