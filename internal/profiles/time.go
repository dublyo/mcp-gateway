package profiles

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type TimeProfile struct{}

func (p *TimeProfile) ID() string { return "time" }

func (p *TimeProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "get_current_time",
			Description: "Get the current time in a timezone",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"timezone": map[string]interface{}{
						"type":        "string",
						"description": "IANA timezone name (e.g. America/New_York, Europe/Berlin). Defaults to UTC.",
					},
				},
			},
		},
		{
			Name:        "convert_timezone",
			Description: "Convert a time between timezones",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"time": map[string]interface{}{
						"type":        "string",
						"description": "Time string in RFC3339 format (e.g. 2024-01-15T14:30:00Z)",
					},
					"from_timezone": map[string]interface{}{
						"type":        "string",
						"description": "Source timezone (IANA format)",
					},
					"to_timezone": map[string]interface{}{
						"type":        "string",
						"description": "Target timezone (IANA format)",
					},
				},
				"required": []string{"time", "to_timezone"},
			},
		},
		{
			Name:        "parse_datetime",
			Description: "Parse a datetime string and return structured information",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"datetime": map[string]interface{}{
						"type":        "string",
						"description": "Datetime string to parse (RFC3339, RFC822, or common formats)",
					},
				},
				"required": []string{"datetime"},
			},
		},
		{
			Name:        "time_difference",
			Description: "Calculate the difference between two times",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"start": map[string]interface{}{
						"type":        "string",
						"description": "Start time in RFC3339 format",
					},
					"end": map[string]interface{}{
						"type":        "string",
						"description": "End time in RFC3339 format",
					},
				},
				"required": []string{"start", "end"},
			},
		},
	}
}

func (p *TimeProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "get_current_time":
		tz := getStr(args, "timezone")
		if tz == "" {
			tz = env["DEFAULT_TIMEZONE"]
		}
		if tz == "" {
			tz = "UTC"
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return "", fmt.Errorf("invalid timezone: %s", tz)
		}
		now := time.Now().In(loc)
		return fmt.Sprintf("Current time in %s:\n%s\nUnix: %d", tz, now.Format(time.RFC3339), now.Unix()), nil

	case "convert_timezone":
		timeStr := getStr(args, "time")
		fromTZ := getStr(args, "from_timezone")
		toTZ := getStr(args, "to_timezone")
		if timeStr == "" || toTZ == "" {
			return "", fmt.Errorf("time and to_timezone are required")
		}
		t, err := time.Parse(time.RFC3339, timeStr)
		if err != nil {
			return "", fmt.Errorf("invalid time format (use RFC3339): %s", err)
		}
		if fromTZ != "" {
			loc, err := time.LoadLocation(fromTZ)
			if err != nil {
				return "", fmt.Errorf("invalid from_timezone: %s", fromTZ)
			}
			t = t.In(loc)
		}
		toLoc, err := time.LoadLocation(toTZ)
		if err != nil {
			return "", fmt.Errorf("invalid to_timezone: %s", toTZ)
		}
		converted := t.In(toLoc)
		return fmt.Sprintf("Converted: %s -> %s\nResult: %s", timeStr, toTZ, converted.Format(time.RFC3339)), nil

	case "parse_datetime":
		dtStr := getStr(args, "datetime")
		if dtStr == "" {
			return "", fmt.Errorf("datetime is required")
		}
		formats := []string{
			time.RFC3339, time.RFC1123, time.RFC822, time.RFC850,
			"2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02",
			"01/02/2006", "Jan 2, 2006",
		}
		var parsed time.Time
		var parseErr error
		for _, f := range formats {
			parsed, parseErr = time.Parse(f, dtStr)
			if parseErr == nil {
				break
			}
		}
		if parseErr != nil {
			return "", fmt.Errorf("could not parse datetime: %s", dtStr)
		}
		return fmt.Sprintf("Parsed: %s\nRFC3339: %s\nUnix: %d\nWeekday: %s\nDay of year: %d",
			dtStr, parsed.Format(time.RFC3339), parsed.Unix(),
			parsed.Weekday().String(), parsed.YearDay()), nil

	case "time_difference":
		startStr := getStr(args, "start")
		endStr := getStr(args, "end")
		if startStr == "" || endStr == "" {
			return "", fmt.Errorf("start and end are required")
		}
		start, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			return "", fmt.Errorf("invalid start time: %s", err)
		}
		end, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			return "", fmt.Errorf("invalid end time: %s", err)
		}
		diff := end.Sub(start)
		hours := math.Abs(diff.Hours())
		days := int(hours / 24)
		remainHours := int(hours) % 24
		mins := int(math.Abs(diff.Minutes())) % 60

		var parts []string
		if days > 0 {
			parts = append(parts, fmt.Sprintf("%d days", days))
		}
		if remainHours > 0 {
			parts = append(parts, fmt.Sprintf("%d hours", remainHours))
		}
		if mins > 0 {
			parts = append(parts, fmt.Sprintf("%d minutes", mins))
		}
		if len(parts) == 0 {
			parts = append(parts, "0 seconds")
		}
		return fmt.Sprintf("Difference: %s\nTotal seconds: %.0f", strings.Join(parts, ", "), math.Abs(diff.Seconds())), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func getStr(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}
