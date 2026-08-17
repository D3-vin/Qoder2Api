package api

import (
	"net"
	"net/http"
	"net/url"

	"qoder2api/auth"
)

const (
	// CosyVersionCLI — from CLI sniff
	CosyVersionCLI = "1.1.20"
	// CosyMachineTypeCLI — CLI machine type (was random 18-char hex)
	CosyMachineTypeCLI = "5"
)

// httpdnsIP returns the target host when it is a literal IP (header omitted otherwise).
func httpdnsIP(targetURL string) string {
	u, err := url.Parse(targetURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if net.ParseIP(host) != nil {
		return host
	}
	return ""
}

func setBearerCLIHeaders(req *http.Request, sess *auth.SessionContext, date, bearer, targetURL string, streamChat bool) {
	req.Header.Set("cosy-data-policy", "agree")
	// req.Header.Set("cosy-data-policy", "AGREE")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cosy-machinetype", sess.MachineType)
	req.Header.Set("cosy-clienttype", "5")
	req.Header.Set("cosy-date", date)
	req.Header.Set("cosy-user", sess.Identity.Uid)
	req.Header.Set("cosy-key", sess.CosyKey)
	req.Header.Set("authorization", bearer)
	req.Header.Set("accept-encoding", "identity")
	req.Header.Set("cosy-version", CosyVersionCLI)
	req.Header.Set("cosy-machineid", sess.MachineId)
	req.Header.Set("cosy-machinetoken", sess.MachineToken)
	req.Header.Set("login-version", "v2")
	req.Header.Set("user-agent", "node")
	// req.Header.Set("user-agent", "Go-http-client/2.0")

	req.Header.Set("cosy-business-product", "cli")
	req.Header.Set("cosy-business-type", "agent")
	req.Header.Set("cosy-scene", "assistant")
	req.Header.Set("cosy-machineos", "x86_64_win32")

	if streamChat {
		req.Header.Set("cache-control", "no-cache")
		req.Header.Set("accept", "text/event-stream")
		// req.Header.Set("cosy-clientip", "169.254.198.161")
	} else {
		req.Header.Set("accept", "application/json")
		// CLI algo GET/POST: Cosy-ClientIp = machine id (chat omits it)
		req.Header.Set("cosy-clientip", sess.MachineId)
		// req.Header.Set("cosy-clientip", "169.254.198.161")
	}

	if ip := httpdnsIP(targetURL); ip != "" {
		req.Header.Set("x-qoder-httpdns-ip", ip)
	}
}
