package profiles

import "sync/atomic"

// OpenChromeBrowserProfile proxies MCP tool calls to an OpenChrome MCP sidecar container.
type OpenChromeBrowserProfile struct {
	requestID atomic.Int64
}

func (p *OpenChromeBrowserProfile) ID() string { return "openchrome-browser" }

func (p *OpenChromeBrowserProfile) Tools() []Tool {
	return []Tool{
		{Name: "navigate", Description: "Navigate to a URL", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"url": map[string]interface{}{"type": "string", "description": "URL to navigate to"}}, "required": []string{"url"}}},
		{Name: "interact", Description: "Click, type, or interact with page elements", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"action": map[string]interface{}{"type": "string", "description": "Action to perform (click, type, etc.)"}, "selector": map[string]interface{}{"type": "string", "description": "CSS selector or element reference"}, "text": map[string]interface{}{"type": "string", "description": "Text to type (for type action)"}}, "required": []string{"action"}}},
		{Name: "find", Description: "Find elements on the page", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"query": map[string]interface{}{"type": "string", "description": "Text or selector to find"}}, "required": []string{"query"}}},
		{Name: "read_page", Description: "Read the full page content or specific sections", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"format": map[string]interface{}{"type": "string", "description": "Output format: text, html, or markdown"}}}},
		{Name: "inspect", Description: "Inspect DOM elements and their properties", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"selector": map[string]interface{}{"type": "string", "description": "CSS selector to inspect"}}, "required": []string{"selector"}}},
		{Name: "query_dom", Description: "Query the DOM with CSS selectors", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"selector": map[string]interface{}{"type": "string", "description": "CSS selector"}, "attributes": map[string]interface{}{"type": "array", "description": "Attributes to return"}}, "required": []string{"selector"}}},
		{Name: "form_input", Description: "Fill a single form field", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"selector": map[string]interface{}{"type": "string", "description": "Field selector"}, "value": map[string]interface{}{"type": "string", "description": "Value to set"}}, "required": []string{"selector", "value"}}},
		{Name: "fill_form", Description: "Fill multiple form fields at once", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"fields": map[string]interface{}{"type": "array", "description": "Array of {selector, value} pairs"}}, "required": []string{"fields"}}},
		{Name: "javascript_tool", Description: "Execute JavaScript in the browser", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"code": map[string]interface{}{"type": "string", "description": "JavaScript code to execute"}}, "required": []string{"code"}}},
		{Name: "computer", Description: "Low-level mouse and keyboard control", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"action": map[string]interface{}{"type": "string", "description": "Action: mouse_click, mouse_move, key_press, etc."}, "x": map[string]interface{}{"type": "number"}, "y": map[string]interface{}{"type": "number"}, "key": map[string]interface{}{"type": "string"}}, "required": []string{"action"}}},
		{Name: "tabs_context", Description: "List and manage browser tabs", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{Name: "tabs_create", Description: "Open a new browser tab", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"url": map[string]interface{}{"type": "string", "description": "URL to open in new tab"}}}},
		{Name: "tabs_close", Description: "Close a browser tab", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"tabId": map[string]interface{}{"type": "string", "description": "Tab ID to close"}}}},
		{Name: "cookies", Description: "Get, set, or delete cookies", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"action": map[string]interface{}{"type": "string", "description": "Action: get, set, delete, or clear"}}, "required": []string{"action"}}},
		{Name: "storage", Description: "Access localStorage and sessionStorage", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"action": map[string]interface{}{"type": "string", "description": "Action: get, set, delete, or clear"}, "type": map[string]interface{}{"type": "string", "description": "Storage type: local or session"}}, "required": []string{"action"}}},
		{Name: "wait_for", Description: "Wait for elements, text, or conditions", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"selector": map[string]interface{}{"type": "string", "description": "CSS selector to wait for"}, "text": map[string]interface{}{"type": "string", "description": "Text to wait for"}, "timeout": map[string]interface{}{"type": "number", "description": "Timeout in milliseconds"}}}},
		{Name: "page_reload", Description: "Reload the current page", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{Name: "lightweight_scroll", Description: "Scroll the page", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"direction": map[string]interface{}{"type": "string", "description": "Direction: up or down"}, "amount": map[string]interface{}{"type": "number", "description": "Pixels to scroll"}}, "required": []string{"direction"}}},
		{Name: "memory", Description: "Store and retrieve session memory", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"action": map[string]interface{}{"type": "string", "description": "Action: get, set, delete, or list"}, "key": map[string]interface{}{"type": "string"}, "value": map[string]interface{}{"type": "string"}}, "required": []string{"action"}}},
		{Name: "network", Description: "Monitor network requests", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{Name: "page_pdf", Description: "Save the page as PDF", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{Name: "page_content", Description: "Get raw HTML content", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}},
		{Name: "file_upload", Description: "Upload files to the page", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"selector": map[string]interface{}{"type": "string", "description": "File input selector"}, "paths": map[string]interface{}{"type": "array", "description": "File paths to upload"}}, "required": []string{"selector", "paths"}}},
		{Name: "click_element", Description: "Click a specific element by selector", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"selector": map[string]interface{}{"type": "string", "description": "CSS selector"}}, "required": []string{"selector"}}},
		{Name: "drag_drop", Description: "Drag and drop elements", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"sourceSelector": map[string]interface{}{"type": "string"}, "targetSelector": map[string]interface{}{"type": "string"}}, "required": []string{"sourceSelector", "targetSelector"}}},
		{Name: "batch_execute", Description: "Execute multiple actions in sequence", InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{"actions": map[string]interface{}{"type": "array", "description": "Array of actions to execute"}}, "required": []string{"actions"}}},
	}
}

func (p *OpenChromeBrowserProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	return proxyToolCall(p, name, args, env)
}

func (p *OpenChromeBrowserProfile) nextID() int64 {
	return p.requestID.Add(1)
}
