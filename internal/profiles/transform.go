package profiles

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type TransformProfile struct{}

func (p *TransformProfile) ID() string { return "transform" }

func (p *TransformProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "json_format",
			Description: "Format (pretty-print) or minify JSON",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"json":   map[string]interface{}{"type": "string", "description": "JSON string to format"},
					"minify": map[string]interface{}{"type": "boolean", "description": "Minify instead of pretty-print (default false)"},
				},
				"required": []string{"json"},
			},
		},
		{
			Name:        "json_query",
			Description: "Extract a value from JSON using a dot-notation path (e.g. 'data.users[0].name')",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"json": map[string]interface{}{"type": "string", "description": "JSON string"},
					"path": map[string]interface{}{"type": "string", "description": "Dot-notation path (e.g. 'data.items[0].id')"},
				},
				"required": []string{"json", "path"},
			},
		},
		{
			Name:        "base64_encode",
			Description: "Encode text to base64",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text":    map[string]interface{}{"type": "string", "description": "Text to encode"},
					"url_safe": map[string]interface{}{"type": "boolean", "description": "Use URL-safe encoding (default false)"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "base64_decode",
			Description: "Decode base64 to text",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"encoded": map[string]interface{}{"type": "string", "description": "Base64 string to decode"},
				},
				"required": []string{"encoded"},
			},
		},
		{
			Name:        "url_encode",
			Description: "URL-encode a string",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{"type": "string", "description": "Text to URL-encode"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "url_decode",
			Description: "URL-decode a string",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{"type": "string", "description": "URL-encoded text to decode"},
				},
				"required": []string{"text"},
			},
		},
		{
			Name:        "json_diff",
			Description: "Compare two JSON objects and show the differences",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"json_a": map[string]interface{}{"type": "string", "description": "First JSON string"},
					"json_b": map[string]interface{}{"type": "string", "description": "Second JSON string"},
				},
				"required": []string{"json_a", "json_b"},
			},
		},
		{
			Name:        "url_parse",
			Description: "Parse a URL into its components (scheme, host, path, query params, etc.)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{"type": "string", "description": "URL to parse"},
				},
				"required": []string{"url"},
			},
		},
	}
}

func (p *TransformProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "json_format":
		return p.jsonFormat(args)
	case "json_query":
		return p.jsonQuery(args)
	case "base64_encode":
		return p.base64Encode(args)
	case "base64_decode":
		return p.base64Decode(args)
	case "url_encode":
		return p.urlEncode(args)
	case "url_decode":
		return p.urlDecode(args)
	case "json_diff":
		return p.jsonDiff(args)
	case "url_parse":
		return p.urlParse(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *TransformProfile) jsonFormat(args map[string]interface{}) (string, error) {
	jsonStr := getStr(args, "json")
	if jsonStr == "" {
		return "", fmt.Errorf("json is required")
	}
	var data interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return "", fmt.Errorf("invalid JSON: %s", err)
	}

	minify, _ := args["minify"].(bool)
	if minify {
		result, _ := json.Marshal(data)
		return string(result), nil
	}
	result, _ := json.MarshalIndent(data, "", "  ")
	return string(result), nil
}

func (p *TransformProfile) jsonQuery(args map[string]interface{}) (string, error) {
	jsonStr := getStr(args, "json")
	path := getStr(args, "path")
	if jsonStr == "" || path == "" {
		return "", fmt.Errorf("json and path are required")
	}
	var data interface{}
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		return "", fmt.Errorf("invalid JSON: %s", err)
	}

	result := navigateJSON(data, path)
	if result == nil {
		return fmt.Sprintf("Path '%s': not found", path), nil
	}

	formatted, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", result), nil
	}
	return string(formatted), nil
}

func navigateJSON(data interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	current := data
	for _, part := range parts {
		if current == nil {
			return nil
		}
		// Handle array index
		if idx := strings.Index(part, "["); idx >= 0 {
			key := part[:idx]
			idxStr := strings.TrimSuffix(part[idx+1:], "]")
			if key != "" {
				m, ok := current.(map[string]interface{})
				if !ok {
					return nil
				}
				current = m[key]
			}
			arr, ok := current.([]interface{})
			if !ok {
				return nil
			}
			var i int
			fmt.Sscanf(idxStr, "%d", &i)
			if i < 0 || i >= len(arr) {
				return nil
			}
			current = arr[i]
		} else {
			m, ok := current.(map[string]interface{})
			if !ok {
				return nil
			}
			current = m[part]
		}
	}
	return current
}

func (p *TransformProfile) base64Encode(args map[string]interface{}) (string, error) {
	text := getStr(args, "text")
	if text == "" {
		return "", fmt.Errorf("text is required")
	}
	urlSafe, _ := args["url_safe"].(bool)
	if urlSafe {
		return base64.URLEncoding.EncodeToString([]byte(text)), nil
	}
	return base64.StdEncoding.EncodeToString([]byte(text)), nil
}

func (p *TransformProfile) base64Decode(args map[string]interface{}) (string, error) {
	encoded := getStr(args, "encoded")
	if encoded == "" {
		return "", fmt.Errorf("encoded is required")
	}
	// Try standard encoding first, then URL-safe
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(encoded)
		if err != nil {
			// Try without padding
			data, err = base64.RawStdEncoding.DecodeString(encoded)
			if err != nil {
				return "", fmt.Errorf("invalid base64: %s", err)
			}
		}
	}
	return string(data), nil
}

func (p *TransformProfile) urlEncode(args map[string]interface{}) (string, error) {
	text := getStr(args, "text")
	if text == "" {
		return "", fmt.Errorf("text is required")
	}
	return url.QueryEscape(text), nil
}

func (p *TransformProfile) urlDecode(args map[string]interface{}) (string, error) {
	text := getStr(args, "text")
	if text == "" {
		return "", fmt.Errorf("text is required")
	}
	decoded, err := url.QueryUnescape(text)
	if err != nil {
		return "", fmt.Errorf("invalid URL encoding: %s", err)
	}
	return decoded, nil
}

func (p *TransformProfile) jsonDiff(args map[string]interface{}) (string, error) {
	jsonA := getStr(args, "json_a")
	jsonB := getStr(args, "json_b")
	if jsonA == "" || jsonB == "" {
		return "", fmt.Errorf("json_a and json_b are required")
	}

	var a, b interface{}
	if err := json.Unmarshal([]byte(jsonA), &a); err != nil {
		return "", fmt.Errorf("invalid json_a: %s", err)
	}
	if err := json.Unmarshal([]byte(jsonB), &b); err != nil {
		return "", fmt.Errorf("invalid json_b: %s", err)
	}

	diffs := diffJSON("", a, b)
	if len(diffs) == 0 {
		return "No differences found — JSONs are identical", nil
	}
	return fmt.Sprintf("Found %d differences:\n\n%s", len(diffs), strings.Join(diffs, "\n")), nil
}

func diffJSON(prefix string, a, b interface{}) []string {
	var diffs []string

	aMap, aIsMap := a.(map[string]interface{})
	bMap, bIsMap := b.(map[string]interface{})
	if aIsMap && bIsMap {
		allKeys := map[string]bool{}
		for k := range aMap {
			allKeys[k] = true
		}
		for k := range bMap {
			allKeys[k] = true
		}
		for k := range allKeys {
			path := k
			if prefix != "" {
				path = prefix + "." + k
			}
			aVal, aHas := aMap[k]
			bVal, bHas := bMap[k]
			if aHas && !bHas {
				diffs = append(diffs, fmt.Sprintf("- %s: %v (removed)", path, aVal))
			} else if !aHas && bHas {
				diffs = append(diffs, fmt.Sprintf("+ %s: %v (added)", path, bVal))
			} else {
				diffs = append(diffs, diffJSON(path, aVal, bVal)...)
			}
		}
		return diffs
	}

	aArr, aIsArr := a.([]interface{})
	bArr, bIsArr := b.([]interface{})
	if aIsArr && bIsArr {
		maxLen := len(aArr)
		if len(bArr) > maxLen {
			maxLen = len(bArr)
		}
		for i := 0; i < maxLen; i++ {
			path := fmt.Sprintf("%s[%d]", prefix, i)
			if i >= len(aArr) {
				diffs = append(diffs, fmt.Sprintf("+ %s: %v (added)", path, bArr[i]))
			} else if i >= len(bArr) {
				diffs = append(diffs, fmt.Sprintf("- %s: %v (removed)", path, aArr[i]))
			} else {
				diffs = append(diffs, diffJSON(path, aArr[i], bArr[i])...)
			}
		}
		return diffs
	}

	aJSON, _ := json.Marshal(a)
	bJSON, _ := json.Marshal(b)
	if string(aJSON) != string(bJSON) {
		path := prefix
		if path == "" {
			path = "(root)"
		}
		diffs = append(diffs, fmt.Sprintf("~ %s: %v -> %v", path, a, b))
	}
	return diffs
}

func (p *TransformProfile) urlParse(args map[string]interface{}) (string, error) {
	rawURL := getStr(args, "url")
	if rawURL == "" {
		return "", fmt.Errorf("url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %s", err)
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("URL: %s", rawURL))
	lines = append(lines, fmt.Sprintf("Scheme: %s", u.Scheme))
	if u.User != nil {
		lines = append(lines, fmt.Sprintf("User: %s", u.User.Username()))
	}
	lines = append(lines, fmt.Sprintf("Host: %s", u.Host))
	lines = append(lines, fmt.Sprintf("Hostname: %s", u.Hostname()))
	if u.Port() != "" {
		lines = append(lines, fmt.Sprintf("Port: %s", u.Port()))
	}
	lines = append(lines, fmt.Sprintf("Path: %s", u.Path))
	if u.RawQuery != "" {
		lines = append(lines, fmt.Sprintf("Query String: %s", u.RawQuery))
		lines = append(lines, "Query Parameters:")
		for key, values := range u.Query() {
			lines = append(lines, fmt.Sprintf("  %s = %s", key, strings.Join(values, ", ")))
		}
	}
	if u.Fragment != "" {
		lines = append(lines, fmt.Sprintf("Fragment: %s", u.Fragment))
	}
	return strings.Join(lines, "\n"), nil
}
