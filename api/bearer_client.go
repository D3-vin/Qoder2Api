package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"qoder2api/auth"
	"qoder2api/encoding"
	"qoder2api/httputil"
)

type BearerApiClient struct {
	sess   *auth.SessionContext
	client *http.Client
}

func NewBearerApiClient(sess *auth.SessionContext) *BearerApiClient {
	return &BearerApiClient{
		sess:   sess,
		client: httputil.NewClient(15 * time.Second),
	}
}

func (c *BearerApiClient) CallPost(fullUrl string, jsonBody interface{}) (map[string]interface{}, error) {
	return c.call("POST", fullUrl, jsonBody, nil)
}

func (c *BearerApiClient) CallGet(fullUrl string) (map[string]interface{}, error) {
	return c.call("GET", fullUrl, nil, nil)
}

// ListActivities fetches promo activities with free quotas (MODEL_FREE_QUOTA).
func (c *BearerApiClient) ListActivities() (map[string]interface{}, error) {
	return c.CallGet("https://api1.qoder.sh/algo/api/v2/activity")
}

// ListModels fetches live model catalog from Qoder CLI model/list.
func (c *BearerApiClient) ListModels() (map[string]interface{}, error) {
	hosts := []string{
		"https://api2.qoder.sh",
		"https://api1.qoder.sh",
		"https://api3.qoder.sh",
	}
	var lastErr error
	for _, host := range hosts {
		url := host + "/algo/api/v2/model/list?Encode=1"
		result, err := c.CallGet(url)
		if err != nil {
			lastErr = err
			fmt.Printf("[models] %s failed: %v\n", host, err)
			continue
		}
		fmt.Printf("[models] loaded from %s keys=%d\n", host, len(result))
		return result, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no model/list hosts tried")
	}
	return nil, lastErr
}

func (c *BearerApiClient) OpenStreamLines(fullUrl string, jsonBody interface{}, extraHeaders map[string]string, onLine func(string) error) error {
	u, err := url.Parse(fullUrl)
	if err != nil {
		return err
	}

	pathQuery := u.Path                     // Use Path instead of RequestURI (no query params)
	pathSig := strings.TrimSpace(pathQuery) // Remove any trailing whitespace
	if strings.HasPrefix(pathQuery, "/algo") {
		pathSig = strings.TrimSpace(pathQuery[len("/algo"):])
	}

	body := ""
	if jsonBody != nil {
		plain, err := json.Marshal(jsonBody)
		if err != nil {
			return err
		}
		body = encoding.Encode(plain)
	}

	payloadB64, err := auth.BuildPayloadB64(c.sess.Info)
	if err != nil {
		return err
	}

	date := fmt.Sprintf("%d", time.Now().Unix())
	sig, err := auth.SignRequest(payloadB64, c.sess.CosyKey, date, body, pathSig)
	if err != nil {
		return err
	}

	bearer := auth.ComposeBearer(payloadB64, sig)

	// Debug output for Bearer requests
	fmt.Printf("[BEARER DEBUG] PathSig: %s\n", pathSig)
	fmt.Printf("[BEARER DEBUG] Date: %s\n", date)

	cosyKeyPreview := c.sess.CosyKey
	if len(cosyKeyPreview) > 20 {
		cosyKeyPreview = cosyKeyPreview[:20]
	}
	fmt.Printf("[BEARER DEBUG] CosyKey: %s\n", cosyKeyPreview)

	payloadB64Preview := payloadB64
	if len(payloadB64Preview) > 50 {
		payloadB64Preview = payloadB64Preview[:50]
	}
	fmt.Printf("[BEARER DEBUG] PayloadB64: %s\n", payloadB64Preview)

	payloadB64Short := payloadB64
	if len(payloadB64Short) > 30 {
		payloadB64Short = payloadB64Short[:30]
	}
	cosyKeyShort := c.sess.CosyKey
	if len(cosyKeyShort) > 20 {
		cosyKeyShort = cosyKeyShort[:20]
	}
	bodyShort := body
	if len(bodyShort) > 20 {
		bodyShort = bodyShort[:20]
	}

	fmt.Printf("[BEARER DEBUG] Signature input: %s\n%s\n%s\n%s\n%s\n", payloadB64Short, cosyKeyShort, date, bodyShort, pathSig)
	fmt.Printf("[BEARER DEBUG] Signature: %s\n", sig)

	bearerPreview := bearer
	if len(bearerPreview) > 50 {
		bearerPreview = bearerPreview[:50]
	}
	fmt.Printf("[BEARER DEBUG] Bearer: %s\n", bearerPreview)
	fmt.Printf("[BEARER DEBUG] POST %s\n", fullUrl)

	req, err := http.NewRequest("POST", fullUrl, bytes.NewBuffer([]byte(body)))
	if err != nil {
		return err
	}

	setBearerCLIHeaders(req, c.sess, date, bearer, fullUrl, true)

	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	started := time.Now()
	resp, err := httputil.NewClient(5 * time.Minute).Do(req)
	if err != nil {
		fmt.Printf("[BEARER DEBUG] Do failed after %s: %v\n", time.Since(started), err)
		return err
	}
	defer resp.Body.Close()
	fmt.Printf("[BEARER DEBUG] response status=%d after %s\n", resp.StatusCode, time.Since(started))
	fmt.Printf("[BEARER DEBUG] content-type=%s encoding=%s\n",
		resp.Header.Get("Content-Type"), resp.Header.Get("Content-Encoding"))

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d %s", resp.StatusCode, string(bodyBytes))
	}

	return c.execHandler(resp, onLine)
}

// streamIdleTimeout bounds how long the SSE stream may stay silent (no bytes)
// before we give up; long generations are fine, only a silent connection is cut.
const streamIdleTimeout = 5 * time.Minute

// isStreamDone reports whether an SSE line cleanly terminates the gateway
// stream: bare or envelope-wrapped [DONE], or the event:finish marker
// (verified live on api1.qoder.sh, see tools/streamprobe).
func isStreamDone(line string) bool {
	if strings.HasPrefix(line, "event:") {
		return strings.TrimSpace(line[len("event:"):]) == "finish"
	}
	if !strings.HasPrefix(line, "data:") {
		return false
	}
	payload := strings.TrimSpace(line[5:])
	if payload == "[DONE]" {
		return true
	}
	var env struct {
		Body string `json:"body"`
	}
	return json.Unmarshal([]byte(payload), &env) == nil && env.Body == "[DONE]"
}

// isStreamErrorEvent reports the gateway's terminal event:error marker.
func isStreamErrorEvent(line string) bool {
	return strings.HasPrefix(line, "event:") && strings.TrimSpace(line[len("event:"):]) == "error"
}

func (c *BearerApiClient) execHandler(resp *http.Response, onLine func(string) error) error {
	// Client bodies support SetReadDeadline since Go 1.20 (HTTP/1.1 and HTTP/2).
	type deadlineSetter interface{ SetReadDeadline(time.Time) error }
	var deadlineSet func(time.Time) error
	if ds, ok := resp.Body.(deadlineSetter); ok {
		deadlineSet = ds.SetReadDeadline
	}
	lineBuf := &bytes.Buffer{}
	buf := make([]byte, 4096)
	started := time.Now()
	var totalBytes, lineCount int
	var sawDone bool
	var streamErr string
	firstLogged := false

	for {
		// ponytail: if the body doesn't support deadlines the call is skipped
		// and the stream runs without idle protection (same as before).
		if deadlineSet != nil {
			_ = deadlineSet(time.Now().Add(streamIdleTimeout))
		}
		n, err := resp.Body.Read(buf)
		if n > 0 {
			totalBytes += n
			if !firstLogged {
				preview := buf[:n]
				if len(preview) > 120 {
					preview = preview[:120]
				}
				fmt.Printf("[stream] first chunk %d bytes after %s: %q\n",
					n, time.Since(started), string(preview))
				firstLogged = true
			}
			for _, b := range buf[:n] {
				if b == '\n' {
					line := lineBuf.String()
					lineBuf.Reset()
					if strings.HasSuffix(line, "\r") {
						line = line[:len(line)-1]
					}
					if line == "" {
						continue
					}
					lineCount++
					if lineCount <= 5 {
						show := line
						if len(show) > 160 {
							show = show[:160] + "..."
						}
						fmt.Printf("[stream] line#%d: %s\n", lineCount, show)
					}
					if isStreamErrorEvent(line) {
						streamErr = "upstream stream failed (event:error)"
					}
					if isStreamDone(line) {
						sawDone = true
					}
					if err := onLine(line); err != nil {
						fmt.Printf("[stream] stopped by handler after %s lines=%d: %v\n",
							time.Since(started), lineCount, err)
						return err
					}
				} else {
					lineBuf.WriteByte(b)
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Printf("[stream] read error after %s bytes=%d lines=%d: %v\n",
				time.Since(started), totalBytes, lineCount, err)
			return err
		}
	}

	fmt.Printf("[stream] read complete after %s bytes=%d lines=%d done=%v\n",
		time.Since(started), totalBytes, lineCount, sawDone)
	if !sawDone {
		if streamErr != "" {
			return fmt.Errorf("%s", streamErr)
		}
		return fmt.Errorf("stream ended without [DONE] marker (connection truncated?)")
	}
	return nil
}

func (c *BearerApiClient) call(method, fullUrl string, jsonBody interface{}, extraHeaders map[string]string) (map[string]interface{}, error) {
	u, err := url.Parse(fullUrl)
	if err != nil {
		return nil, err
	}

	pathQuery := u.Path                     // Use Path instead of RequestURI (no query params)
	pathSig := strings.TrimSpace(pathQuery) // Remove any trailing whitespace
	if strings.HasPrefix(pathQuery, "/algo") {
		pathSig = strings.TrimSpace(pathQuery[len("/algo"):])
	}

	body := ""
	if jsonBody != nil {
		plain, err := json.Marshal(jsonBody)
		if err != nil {
			return nil, err
		}
		body = encoding.Encode(plain)
	}

	payloadB64, err := auth.BuildPayloadB64(c.sess.Info)
	if err != nil {
		return nil, err
	}

	date := fmt.Sprintf("%d", time.Now().Unix())
	sig, err := auth.SignRequest(payloadB64, c.sess.CosyKey, date, body, pathSig)
	if err != nil {
		return nil, err
	}

	bearer := auth.ComposeBearer(payloadB64, sig)

	// Debug output for call method
	fmt.Printf("[CALL DEBUG] Method: %s, URL: %s\n", method, fullUrl)
	fmt.Printf("[CALL DEBUG] PathSig: %s\n", pathSig)
	fmt.Printf("[CALL DEBUG] Date: %s\n", date)
	fmt.Printf("[CALL DEBUG] Signature: %s\n", sig)

	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewBuffer([]byte(body))
	}

	req, err := http.NewRequest(method, fullUrl, bodyReader)
	if err != nil {
		return nil, err
	}

	setBearerCLIHeaders(req, c.sess, date, bearer, fullUrl, false)

	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d body=%s", resp.StatusCode, string(bodyBytes))
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err == nil {
		return result, nil
	}

	// Encode=1 responses may be Qoder-custom-encoded
	decoded, decErr := encoding.Decode(string(respBody))
	if decErr != nil {
		return nil, fmt.Errorf("json decode failed and encode decode failed: %v / %v; body=%s", err, decErr, truncate(string(respBody), 200))
	}
	if err := json.Unmarshal(decoded, &result); err != nil {
		return nil, fmt.Errorf("decoded body is not json: %v; body=%s", err, truncate(string(decoded), 200))
	}
	return result, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
