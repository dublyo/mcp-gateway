package profiles

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type FetchProfile struct{}

func (p *FetchProfile) ID() string { return "fetch" }

func (p *FetchProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "fetch_url",
			Description: "Fetch a URL and return the response body as text",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to fetch",
					},
					"method": map[string]interface{}{
						"type":        "string",
						"description": "HTTP method (GET, POST, etc.). Defaults to GET.",
						"default":     "GET",
					},
					"headers": map[string]interface{}{
						"type":        "object",
						"description": "Custom headers to include",
					},
					"body": map[string]interface{}{
						"type":        "string",
						"description": "Request body (for POST/PUT)",
					},
				},
				"required": []string{"url"},
			},
		},
		{
			Name:        "fetch_html",
			Description: "Fetch a URL and extract readable text content (strips HTML tags)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to fetch",
					},
				},
				"required": []string{"url"},
			},
		},
	}
}

func (p *FetchProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "fetch_url":
		return p.fetchURL(args, env)
	case "fetch_html":
		return p.fetchHTML(args, env)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *FetchProfile) fetchURL(args map[string]interface{}, env map[string]string) (string, error) {
	rawURL := getStr(args, "url")
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	if err := validateURL(rawURL, env); err != nil {
		return "", err
	}

	method := getStr(args, "method")
	if method == "" {
		method = "GET"
	}

	body := getStr(args, "body")
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, rawURL, bodyReader)
	if err != nil {
		return "", fmt.Errorf("invalid request: %s", err)
	}

	ua := env["USER_AGENT"]
	if ua == "" {
		ua = "Dublyo-MCP-Fetch/1.0"
	}
	req.Header.Set("User-Agent", ua)

	// Custom headers
	if h, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range h {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}

	maxSize := 5 * 1024 * 1024
	if ms := env["MAX_RESPONSE_SIZE"]; ms != "" {
		if n, err := strconv.Atoi(ms); err == nil {
			maxSize = n
		}
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %s", err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, int64(maxSize))
	data, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read failed: %s", err)
	}

	return fmt.Sprintf("Status: %d %s\nContent-Type: %s\nContent-Length: %d\n\n%s",
		resp.StatusCode, resp.Status, resp.Header.Get("Content-Type"), len(data), string(data)), nil
}

func (p *FetchProfile) fetchHTML(args map[string]interface{}, env map[string]string) (string, error) {
	rawURL := getStr(args, "url")
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}
	if err := validateURL(rawURL, env); err != nil {
		return "", err
	}

	ua := env["USER_AGENT"]
	if ua == "" {
		ua = "Dublyo-MCP-Fetch/1.0"
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("invalid request: %s", err)
	}
	req.Header.Set("User-Agent", ua)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %s", err)
	}
	defer resp.Body.Close()

	maxSize := 5 * 1024 * 1024
	if ms := env["MAX_RESPONSE_SIZE"]; ms != "" {
		if n, err := strconv.Atoi(ms); err == nil {
			maxSize = n
		}
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxSize)))
	if err != nil {
		return "", fmt.Errorf("read failed: %s", err)
	}

	// Simple HTML tag stripping
	text := stripHTML(string(data))
	return fmt.Sprintf("URL: %s\nStatus: %d\n\n%s", rawURL, resp.StatusCode, text), nil
}

func validateURL(rawURL string, env map[string]string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %s", err)
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http/https URLs are supported")
	}

	// SSRF prevention: block private IP ranges
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("access to private/local IPs is blocked")
		}
	}

	// Domain whitelist
	if allowed := env["ALLOWED_DOMAINS"]; allowed != "" {
		domains := strings.Split(allowed, ",")
		found := false
		for _, d := range domains {
			d = strings.TrimSpace(d)
			if d != "" && (host == d || strings.HasSuffix(host, "."+d)) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("domain %s is not in the allowed list", host)
		}
	}

	return nil
}

func stripHTML(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			result.WriteRune(' ')
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	// Collapse whitespace
	text := result.String()
	lines := strings.Split(text, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return strings.Join(cleaned, "\n")
}
