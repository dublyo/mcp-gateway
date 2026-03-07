package profiles

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// PlaywrightBrowserProfile proxies MCP tool calls to a Playwright MCP sidecar container.
type PlaywrightBrowserProfile struct {
	requestID atomic.Int64
}

func (p *PlaywrightBrowserProfile) ID() string { return "playwright-browser" }

func (p *PlaywrightBrowserProfile) Tools() []Tool {
	return []Tool{
		{Name: "browser_navigate", Description: "Navigate to a URL", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"url": map[string]interface{}{"type": "string", "description": "The URL to navigate to"}}, "required": []string{"url"}}},
		{Name: "browser_click", Description: "Click an element on the page", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"ref": map[string]interface{}{"type": "string", "description": "Element reference from page snapshot"}, "element": map[string]interface{}{"type": "string", "description": "Human-readable element description"}}, "required": []string{"ref"}}},
		{Name: "browser_type", Description: "Type text into an editable element", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"ref": map[string]interface{}{"type": "string", "description": "Element reference from page snapshot"}, "text": map[string]interface{}{"type": "string", "description": "Text to type"}, "submit": map[string]interface{}{"type": "boolean", "description": "Press Enter after typing"}}, "required": []string{"ref", "text"}}},
		{Name: "browser_snapshot", Description: "Capture accessibility snapshot of the page", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{Name: "browser_take_screenshot", Description: "Take a screenshot of the current page", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"type": map[string]interface{}{"type": "string", "description": "Image format (png or jpeg)", "default": "png"}, "fullPage": map[string]interface{}{"type": "boolean", "description": "Capture full scrollable page"}}}},
		{Name: "browser_hover", Description: "Hover over an element", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"ref": map[string]interface{}{"type": "string", "description": "Element reference from page snapshot"}}, "required": []string{"ref"}}},
		{Name: "browser_fill_form", Description: "Fill multiple form fields", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"fields": map[string]interface{}{"type": "array", "description": "Fields to fill in"}}, "required": []string{"fields"}}},
		{Name: "browser_select_option", Description: "Select an option in a dropdown", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"ref": map[string]interface{}{"type": "string", "description": "Element reference"}, "values": map[string]interface{}{"type": "array", "description": "Values to select"}}, "required": []string{"ref", "values"}}},
		{Name: "browser_evaluate", Description: "Evaluate JavaScript on the page", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"function": map[string]interface{}{"type": "string", "description": "JavaScript function to evaluate"}}, "required": []string{"function"}}},
		{Name: "browser_run_code", Description: "Run a Playwright code snippet", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"code": map[string]interface{}{"type": "string", "description": "Playwright code to execute, e.g. async (page) => { ... }"}}, "required": []string{"code"}}},
		{Name: "browser_press_key", Description: "Press a key on the keyboard", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"key": map[string]interface{}{"type": "string", "description": "Key name like ArrowLeft or a character"}}, "required": []string{"key"}}},
		{Name: "browser_drag", Description: "Drag and drop between elements", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"startRef": map[string]interface{}{"type": "string", "description": "Source element ref"}, "endRef": map[string]interface{}{"type": "string", "description": "Target element ref"}}, "required": []string{"startRef", "endRef"}}},
		{Name: "browser_file_upload", Description: "Upload files", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"paths": map[string]interface{}{"type": "array", "description": "File paths to upload"}}}},
		{Name: "browser_handle_dialog", Description: "Handle a dialog", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"accept": map[string]interface{}{"type": "boolean", "description": "Whether to accept the dialog"}}, "required": []string{"accept"}}},
		{Name: "browser_tabs", Description: "Manage browser tabs", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"action": map[string]interface{}{"type": "string", "description": "Operation: list, create, close, or select"}}, "required": []string{"action"}}},
		{Name: "browser_navigate_back", Description: "Go back to the previous page", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{Name: "browser_wait_for", Description: "Wait for text or timeout", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"time": map[string]interface{}{"type": "number", "description": "Seconds to wait"}, "text": map[string]interface{}{"type": "string", "description": "Text to wait for"}}}},
		{Name: "browser_resize", Description: "Resize the browser window", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"width": map[string]interface{}{"type": "number"}, "height": map[string]interface{}{"type": "number"}}, "required": []string{"width", "height"}}},
		{Name: "browser_console_messages", Description: "Get console messages", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{Name: "browser_network_requests", Description: "List network requests", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{Name: "browser_close", Description: "Close the browser page", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
	}
}

func (p *PlaywrightBrowserProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	return proxyToolCall(p, name, args, env)
}

func (p *PlaywrightBrowserProfile) nextID() int64 {
	return p.requestID.Add(1)
}

// proxyToolCall sends a JSON-RPC tools/call request to the upstream MCP sidecar.
func proxyToolCall(p interface{ nextID() int64 }, toolName string, args map[string]interface{}, env map[string]string) (string, error) {
	upstream := env["MCP_UPSTREAM_URL"]
	if upstream == "" {
		return "", fmt.Errorf("MCP_UPSTREAM_URL is not configured — deploy the browser container first")
	}

	reqID := p.nextID()
	rpcReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}

	body, err := json.Marshal(rpcReq)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Post(upstream, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read upstream response: %w", err)
	}

	// Parse the JSON-RPC response
	var rpcResp struct {
		Result *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		// If we can't parse as JSON-RPC, return raw response
		return string(respBody), nil
	}

	if rpcResp.Error != nil {
		return "", fmt.Errorf("upstream error: %s", rpcResp.Error.Message)
	}

	if rpcResp.Result != nil && len(rpcResp.Result.Content) > 0 {
		var texts []string
		for _, c := range rpcResp.Result.Content {
			if c.Text != "" {
				texts = append(texts, c.Text)
			}
		}
		if len(texts) > 0 {
			return texts[0], nil
		}
	}

	return string(respBody), nil
}
