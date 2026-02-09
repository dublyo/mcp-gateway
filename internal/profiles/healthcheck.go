package profiles

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type HealthcheckProfile struct{}

func (p *HealthcheckProfile) ID() string { return "healthcheck" }

func (p *HealthcheckProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "ping_url",
			Description: "Check if a URL is reachable and measure response time",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url":    map[string]interface{}{"type": "string", "description": "URL to check"},
					"method": map[string]interface{}{"type": "string", "description": "HTTP method (default GET)"},
				},
				"required": []string{"url"},
			},
		},
		{
			Name:        "check_ssl",
			Description: "Check SSL/TLS certificate details and expiry for a domain",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"domain": map[string]interface{}{"type": "string", "description": "Domain to check SSL certificate"},
					"port":   map[string]interface{}{"type": "integer", "description": "Port (default 443)"},
				},
				"required": []string{"domain"},
			},
		},
		{
			Name:        "check_headers",
			Description: "Fetch and display HTTP response headers for a URL",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{"type": "string", "description": "URL to fetch headers from"},
				},
				"required": []string{"url"},
			},
		},
		{
			Name:        "check_redirect_chain",
			Description: "Follow and display the full redirect chain for a URL",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{"type": "string", "description": "URL to follow redirects"},
				},
				"required": []string{"url"},
			},
		},
	}
}

func (p *HealthcheckProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "ping_url":
		return p.pingURL(args)
	case "check_ssl":
		return p.checkSSL(args)
	case "check_headers":
		return p.checkHeaders(args)
	case "check_redirect_chain":
		return p.checkRedirectChain(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *HealthcheckProfile) pingURL(args map[string]interface{}) (string, error) {
	rawURL := getStr(args, "url")
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}
	method := getStr(args, "method")
	if method == "" {
		method = "GET"
	}

	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid request: %s", err)
	}
	req.Header.Set("User-Agent", "Dublyo-Healthcheck/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return fmt.Sprintf("URL: %s\nStatus: UNREACHABLE\nError: %s\nResponse Time: %s", rawURL, err, elapsed.Round(time.Millisecond)), nil
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	status := "UP"
	if resp.StatusCode >= 400 {
		status = "DOWN"
	}

	return fmt.Sprintf("URL: %s\nStatus: %s\nHTTP Status: %d %s\nResponse Time: %s\nContent-Type: %s\nServer: %s",
		rawURL, status, resp.StatusCode, http.StatusText(resp.StatusCode),
		elapsed.Round(time.Millisecond),
		resp.Header.Get("Content-Type"),
		resp.Header.Get("Server")), nil
}

func (p *HealthcheckProfile) checkSSL(args map[string]interface{}) (string, error) {
	domain := getStr(args, "domain")
	if domain == "" {
		return "", fmt.Errorf("domain is required")
	}
	port := int(getFloat(args, "port"))
	if port <= 0 {
		port = 443
	}

	addr := fmt.Sprintf("%s:%d", domain, port)
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second}, "tcp", addr, &tls.Config{})
	if err != nil {
		return fmt.Sprintf("SSL check for %s:\nStatus: FAILED\nError: %s", domain, err), nil
	}
	defer conn.Close()

	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return fmt.Sprintf("SSL check for %s: No certificates found", domain), nil
	}

	cert := state.PeerCertificates[0]
	now := time.Now()
	daysUntilExpiry := int(cert.NotAfter.Sub(now).Hours() / 24)
	expiryStatus := "VALID"
	if daysUntilExpiry < 0 {
		expiryStatus = "EXPIRED"
	} else if daysUntilExpiry < 30 {
		expiryStatus = "EXPIRING SOON"
	}

	var sans []string
	for _, name := range cert.DNSNames {
		sans = append(sans, name)
	}

	var chain []string
	for _, c := range state.PeerCertificates {
		chain = append(chain, fmt.Sprintf("  - %s (issuer: %s)", c.Subject.CommonName, c.Issuer.CommonName))
	}

	return fmt.Sprintf("SSL Certificate for %s:\n\nSubject: %s\nIssuer: %s\nValid From: %s\nValid Until: %s\nDays Until Expiry: %d (%s)\nSANs: %s\nTLS Version: %s\nCipher Suite: %s\n\nCertificate Chain:\n%s",
		domain,
		cert.Subject.CommonName,
		cert.Issuer.CommonName,
		cert.NotBefore.Format("2006-01-02"),
		cert.NotAfter.Format("2006-01-02"),
		daysUntilExpiry, expiryStatus,
		strings.Join(sans, ", "),
		tlsVersionString(state.Version),
		tls.CipherSuiteName(state.CipherSuite),
		strings.Join(chain, "\n")), nil
}

func (p *HealthcheckProfile) checkHeaders(args map[string]interface{}) (string, error) {
	rawURL := getStr(args, "url")
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	req, err := http.NewRequest("HEAD", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid request: %s", err)
	}
	req.Header.Set("User-Agent", "Dublyo-Healthcheck/1.0")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %s", err)
	}
	defer resp.Body.Close()

	var lines []string
	lines = append(lines, fmt.Sprintf("URL: %s", rawURL))
	lines = append(lines, fmt.Sprintf("Status: %d %s", resp.StatusCode, http.StatusText(resp.StatusCode)))
	lines = append(lines, "")
	lines = append(lines, "Headers:")

	securityHeaders := map[string]bool{
		"Strict-Transport-Security": false,
		"Content-Security-Policy":   false,
		"X-Content-Type-Options":    false,
		"X-Frame-Options":           false,
		"X-XSS-Protection":          false,
	}

	for key, values := range resp.Header {
		lines = append(lines, fmt.Sprintf("  %s: %s", key, strings.Join(values, ", ")))
		if _, ok := securityHeaders[key]; ok {
			securityHeaders[key] = true
		}
	}

	lines = append(lines, "")
	lines = append(lines, "Security Headers:")
	for header, present := range securityHeaders {
		icon := "✗"
		if present {
			icon = "✓"
		}
		lines = append(lines, fmt.Sprintf("  %s %s", icon, header))
	}

	return strings.Join(lines, "\n"), nil
}

func (p *HealthcheckProfile) checkRedirectChain(args map[string]interface{}) (string, error) {
	rawURL := getStr(args, "url")
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	var chain []string
	currentURL := rawURL

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	for i := 0; i < 10; i++ {
		req, err := http.NewRequest("GET", currentURL, nil)
		if err != nil {
			break
		}
		req.Header.Set("User-Agent", "Dublyo-Healthcheck/1.0")

		resp, err := client.Do(req)
		if err != nil {
			chain = append(chain, fmt.Sprintf("%d. %s -> ERROR: %s", i+1, currentURL, err))
			break
		}
		resp.Body.Close()

		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location := resp.Header.Get("Location")
			chain = append(chain, fmt.Sprintf("%d. %s -> %d -> %s", i+1, currentURL, resp.StatusCode, location))
			currentURL = location
		} else {
			chain = append(chain, fmt.Sprintf("%d. %s -> %d (final)", i+1, currentURL, resp.StatusCode))
			break
		}
	}

	return fmt.Sprintf("Redirect chain for %s:\n\n%s", rawURL, strings.Join(chain, "\n")), nil
}

func tlsVersionString(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("Unknown (0x%04x)", v)
	}
}
