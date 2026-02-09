package profiles

import (
	"fmt"
	"regexp"
	"strings"
)

type RegexProfile struct{}

func (p *RegexProfile) ID() string { return "regex" }

func (p *RegexProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "test_regex",
			Description: "Test a regular expression against a string and return matches",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{"type": "string", "description": "Regular expression pattern"},
					"text":    map[string]interface{}{"type": "string", "description": "Text to test against"},
					"flags":   map[string]interface{}{"type": "string", "description": "Flags: i (case-insensitive), s (dot matches newline), m (multiline)"},
				},
				"required": []string{"pattern", "text"},
			},
		},
		{
			Name:        "extract_matches",
			Description: "Extract all matches of a pattern from text",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{"type": "string", "description": "Regular expression pattern"},
					"text":    map[string]interface{}{"type": "string", "description": "Text to extract from"},
					"flags":   map[string]interface{}{"type": "string", "description": "Flags: i, s, m"},
				},
				"required": []string{"pattern", "text"},
			},
		},
		{
			Name:        "replace_regex",
			Description: "Replace matches of a pattern in text",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern":     map[string]interface{}{"type": "string", "description": "Regular expression pattern"},
					"text":        map[string]interface{}{"type": "string", "description": "Text to perform replacement on"},
					"replacement": map[string]interface{}{"type": "string", "description": "Replacement string (use $1, $2 for groups)"},
					"flags":       map[string]interface{}{"type": "string", "description": "Flags: i, s, m"},
				},
				"required": []string{"pattern", "text", "replacement"},
			},
		},
		{
			Name:        "split_regex",
			Description: "Split text by a regular expression pattern",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{"type": "string", "description": "Regular expression pattern to split on"},
					"text":    map[string]interface{}{"type": "string", "description": "Text to split"},
					"limit":   map[string]interface{}{"type": "integer", "description": "Maximum number of splits (-1 for unlimited, default -1)"},
				},
				"required": []string{"pattern", "text"},
			},
		},
	}
}

func (p *RegexProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "test_regex":
		return p.testRegex(args)
	case "extract_matches":
		return p.extractMatches(args)
	case "replace_regex":
		return p.replaceRegex(args)
	case "split_regex":
		return p.splitRegex(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func compilePattern(pattern, flags string) (*regexp.Regexp, error) {
	prefix := ""
	if strings.Contains(flags, "i") {
		prefix += "(?i)"
	}
	if strings.Contains(flags, "s") {
		prefix += "(?s)"
	}
	if strings.Contains(flags, "m") {
		prefix += "(?m)"
	}
	re, err := regexp.Compile(prefix + pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %s", err)
	}
	return re, nil
}

func (p *RegexProfile) testRegex(args map[string]interface{}) (string, error) {
	pattern := getStr(args, "pattern")
	text := getStr(args, "text")
	flags := getStr(args, "flags")
	if pattern == "" || text == "" {
		return "", fmt.Errorf("pattern and text are required")
	}

	re, err := compilePattern(pattern, flags)
	if err != nil {
		return "", err
	}

	matches := re.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return fmt.Sprintf("Pattern: /%s/\nResult: NO MATCH", pattern), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Pattern: /%s/", pattern))
	lines = append(lines, fmt.Sprintf("Matches: %d", len(matches)))
	lines = append(lines, "")

	subNames := re.SubexpNames()
	for i, match := range matches {
		if i >= 20 {
			lines = append(lines, fmt.Sprintf("... and %d more matches", len(matches)-20))
			break
		}
		fullMatch := text[match[0]:match[1]]
		lines = append(lines, fmt.Sprintf("Match %d: \"%s\" (index %d-%d)", i+1, fullMatch, match[0], match[1]))

		for g := 2; g < len(match); g += 2 {
			if match[g] >= 0 {
				groupIdx := g / 2
				groupName := ""
				if groupIdx < len(subNames) && subNames[groupIdx] != "" {
					groupName = fmt.Sprintf(" (%s)", subNames[groupIdx])
				}
				lines = append(lines, fmt.Sprintf("  Group %d%s: \"%s\"", groupIdx, groupName, text[match[g]:match[g+1]]))
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}

func (p *RegexProfile) extractMatches(args map[string]interface{}) (string, error) {
	pattern := getStr(args, "pattern")
	text := getStr(args, "text")
	flags := getStr(args, "flags")
	if pattern == "" || text == "" {
		return "", fmt.Errorf("pattern and text are required")
	}

	re, err := compilePattern(pattern, flags)
	if err != nil {
		return "", err
	}

	allMatches := re.FindAllStringSubmatch(text, -1)
	if len(allMatches) == 0 {
		return "No matches found", nil
	}

	var results []string
	for i, match := range allMatches {
		if i >= 100 {
			results = append(results, fmt.Sprintf("... truncated (%d total matches)", len(allMatches)))
			break
		}
		if len(match) > 1 {
			results = append(results, fmt.Sprintf("%d. %s (groups: %s)", i+1, match[0], strings.Join(match[1:], ", ")))
		} else {
			results = append(results, fmt.Sprintf("%d. %s", i+1, match[0]))
		}
	}
	return fmt.Sprintf("Found %d matches:\n%s", len(allMatches), strings.Join(results, "\n")), nil
}

func (p *RegexProfile) replaceRegex(args map[string]interface{}) (string, error) {
	pattern := getStr(args, "pattern")
	text := getStr(args, "text")
	replacement := getStr(args, "replacement")
	flags := getStr(args, "flags")
	if pattern == "" || text == "" {
		return "", fmt.Errorf("pattern and text are required")
	}

	re, err := compilePattern(pattern, flags)
	if err != nil {
		return "", err
	}

	result := re.ReplaceAllString(text, replacement)
	count := len(re.FindAllStringIndex(text, -1))

	return fmt.Sprintf("Replacements made: %d\n\nResult:\n%s", count, result), nil
}

func (p *RegexProfile) splitRegex(args map[string]interface{}) (string, error) {
	pattern := getStr(args, "pattern")
	text := getStr(args, "text")
	if pattern == "" || text == "" {
		return "", fmt.Errorf("pattern and text are required")
	}

	limit := int(getFloat(args, "limit"))
	if limit == 0 {
		limit = -1
	}

	re, err := compilePattern(pattern, "")
	if err != nil {
		return "", err
	}

	parts := re.Split(text, limit)
	var lines []string
	for i, part := range parts {
		lines = append(lines, fmt.Sprintf("%d. \"%s\"", i+1, part))
	}
	return fmt.Sprintf("Split into %d parts:\n%s", len(parts), strings.Join(lines, "\n")), nil
}
