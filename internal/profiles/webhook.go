package profiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type WebhookProfile struct{}

func (p *WebhookProfile) ID() string { return "webhook" }

func (p *WebhookProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "send_webhook",
			Description: "Send an HTTP webhook (POST JSON to a URL)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url":     map[string]interface{}{"type": "string", "description": "Webhook URL to send to"},
					"payload": map[string]interface{}{"type": "object", "description": "JSON payload to send"},
					"method":  map[string]interface{}{"type": "string", "description": "HTTP method (default POST)"},
					"headers": map[string]interface{}{"type": "object", "description": "Custom headers"},
				},
				"required": []string{"url", "payload"},
			},
		},
		{
			Name:        "send_slack",
			Description: "Send a message to Slack via webhook",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text":    map[string]interface{}{"type": "string", "description": "Message text (supports Slack markdown)"},
					"channel": map[string]interface{}{"type": "string", "description": "Override channel (optional)"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "send_discord",
			Description: "Send a message to Discord via webhook",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content":  map[string]interface{}{"type": "string", "description": "Message content (supports Discord markdown)"},
					"username": map[string]interface{}{"type": "string", "description": "Override bot username (optional)"},
				},
				"required": []string{"content"},
			},
		},
	}
}

func (p *WebhookProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "send_webhook":
		return p.sendWebhook(args, env)
	case "send_slack":
		return p.sendSlack(args, env)
	case "send_discord":
		return p.sendDiscord(args, env)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *WebhookProfile) sendWebhook(args map[string]interface{}, env map[string]string) (string, error) {
	rawURL := getStr(args, "url")
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	// Check allowed URLs
	if allowed := env["ALLOWED_URLS"]; allowed != "" {
		domains := strings.Split(allowed, ",")
		found := false
		for _, d := range domains {
			d = strings.TrimSpace(d)
			if d != "" && strings.Contains(rawURL, d) {
				found = true
				break
			}
		}
		if !found {
			return "", fmt.Errorf("URL not in allowed list")
		}
	}

	payload, _ := args["payload"]
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("invalid payload: %s", err)
	}

	method := getStr(args, "method")
	if method == "" {
		method = "POST"
	}

	req, err := http.NewRequest(method, rawURL, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("invalid request: %s", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Dublyo-MCP-Webhook/1.0")

	if h, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range h {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("webhook failed: %s", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	return fmt.Sprintf("Webhook sent!\nURL: %s\nMethod: %s\nStatus: %d %s\nResponse: %s",
		rawURL, method, resp.StatusCode, http.StatusText(resp.StatusCode), string(body)), nil
}

func (p *WebhookProfile) sendSlack(args map[string]interface{}, env map[string]string) (string, error) {
	webhookURL := env["SLACK_WEBHOOK_URL"]
	if webhookURL == "" {
		return "", fmt.Errorf("SLACK_WEBHOOK_URL environment variable is not configured")
	}

	text := getStr(args, "text")
	if text == "" {
		return "", fmt.Errorf("text is required")
	}

	payload := map[string]interface{}{"text": text}
	if ch := getStr(args, "channel"); ch != "" {
		payload["channel"] = ch
	}

	data, _ := json.Marshal(payload)
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("slack webhook failed: %s", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	if resp.StatusCode == 200 {
		return fmt.Sprintf("Slack message sent successfully!\nText: %s", text), nil
	}
	return fmt.Sprintf("Slack webhook returned %d: %s", resp.StatusCode, string(body)), nil
}

func (p *WebhookProfile) sendDiscord(args map[string]interface{}, env map[string]string) (string, error) {
	webhookURL := env["DISCORD_WEBHOOK_URL"]
	if webhookURL == "" {
		return "", fmt.Errorf("DISCORD_WEBHOOK_URL environment variable is not configured")
	}

	content := getStr(args, "content")
	if content == "" {
		return "", fmt.Errorf("content is required")
	}

	payload := map[string]interface{}{"content": content}
	if username := getStr(args, "username"); username != "" {
		payload["username"] = username
	}

	data, _ := json.Marshal(payload)
	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("discord webhook failed: %s", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))

	if resp.StatusCode == 204 || resp.StatusCode == 200 {
		return fmt.Sprintf("Discord message sent successfully!\nContent: %s", content), nil
	}
	return fmt.Sprintf("Discord webhook returned %d: %s", resp.StatusCode, string(body)), nil
}
