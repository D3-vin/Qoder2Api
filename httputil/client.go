package httputil

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	transportOnce sync.Once
	baseTransport *http.Transport
)

func envEnabled(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func sharedTransport() *http.Transport {
	transportOnce.Do(func() {
		baseTransport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: envEnabled("QODER_INSECURE_SKIP_VERIFY"),
			},
		}
		if envEnabled("QODER_PROXY_ENABLED") {
			proxyURL := strings.TrimSpace(os.Getenv("QODER_PROXY_URL"))
			if proxyURL == "" {
				fmt.Println("[http] QODER_PROXY_ENABLED=true but QODER_PROXY_URL is empty")
			} else if u, err := url.Parse(proxyURL); err != nil {
				fmt.Printf("[http] invalid QODER_PROXY_URL: %v\n", err)
			} else {
				baseTransport.Proxy = http.ProxyURL(u)
				fmt.Printf("[http] proxy enabled: %s\n", proxyURL)
			}
		}
		if envEnabled("QODER_INSECURE_SKIP_VERIFY") {
			fmt.Println("[http] TLS certificate verification disabled")
		}
	})
	return baseTransport
}

func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: sharedTransport(),
	}
}
