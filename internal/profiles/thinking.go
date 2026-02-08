package profiles

import (
	"fmt"
	"strings"
)

type ThinkingProfile struct{}

func (p *ThinkingProfile) ID() string { return "thinking" }

func (p *ThinkingProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "think",
			Description: "Record a thinking step with structured reasoning. Use this to break down complex problems step by step.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"thought": map[string]interface{}{
						"type":        "string",
						"description": "The current thinking step or reasoning",
					},
					"step_number": map[string]interface{}{
						"type":        "integer",
						"description": "Step number in the reasoning chain",
					},
					"total_steps": map[string]interface{}{
						"type":        "integer",
						"description": "Expected total number of steps (can be revised)",
					},
					"next_action": map[string]interface{}{
						"type":        "string",
						"description": "What to do next based on this thinking step",
					},
				},
				"required": []string{"thought"},
			},
		},
	}
}

func (p *ThinkingProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	if name != "think" {
		return "", fmt.Errorf("unknown tool: %s", name)
	}

	thought := getStr(args, "thought")
	if thought == "" {
		return "", fmt.Errorf("thought is required")
	}

	var parts []string
	parts = append(parts, fmt.Sprintf("Thought: %s", thought))

	if stepNum, ok := args["step_number"]; ok {
		if total, ok := args["total_steps"]; ok {
			parts = append(parts, fmt.Sprintf("Step: %v of %v", stepNum, total))
		} else {
			parts = append(parts, fmt.Sprintf("Step: %v", stepNum))
		}
	}

	if next := getStr(args, "next_action"); next != "" {
		parts = append(parts, fmt.Sprintf("Next: %s", next))
	}

	return strings.Join(parts, "\n"), nil
}
