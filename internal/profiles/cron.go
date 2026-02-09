package profiles

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type CronProfile struct{}

func (p *CronProfile) ID() string { return "cron" }

func (p *CronProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "parse_cron",
			Description: "Parse a cron expression and explain it in human-readable terms",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"expression": map[string]interface{}{"type": "string", "description": "Cron expression (5 fields: min hour dom month dow)"},
				},
				"required": []string{"expression"},
			},
		},
		{
			Name:        "next_runs",
			Description: "Calculate the next N run times for a cron expression",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"expression": map[string]interface{}{"type": "string", "description": "Cron expression (5 fields)"},
					"count":      map[string]interface{}{"type": "integer", "description": "Number of next runs to show (default 5, max 20)"},
					"timezone":   map[string]interface{}{"type": "string", "description": "IANA timezone (default UTC)"},
				},
				"required": []string{"expression"},
			},
		},
		{
			Name:        "cron_builder",
			Description: "Build a cron expression from human-readable schedule description",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"schedule": map[string]interface{}{
						"type":        "string",
						"description": "Schedule description. Supported: 'every N minutes', 'every N hours', 'daily at HH:MM', 'weekly on DAY at HH:MM', 'monthly on DAY at HH:MM', 'hourly', 'midnight', 'noon'",
					},
				},
				"required": []string{"schedule"},
			},
		},
	}
}

func (p *CronProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "parse_cron":
		return p.parseCron(args)
	case "next_runs":
		return p.nextRuns(args)
	case "cron_builder":
		return p.cronBuilder(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *CronProfile) parseCron(args map[string]interface{}) (string, error) {
	expr := getStr(args, "expression")
	if expr == "" {
		return "", fmt.Errorf("expression is required")
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return "", fmt.Errorf("cron expression must have 5 fields (minute hour day-of-month month day-of-week), got %d", len(fields))
	}

	fieldNames := []string{"Minute", "Hour", "Day of Month", "Month", "Day of Week"}
	fieldRanges := []string{"0-59", "0-23", "1-31", "1-12", "0-6 (Sun=0)"}

	var lines []string
	lines = append(lines, fmt.Sprintf("Expression: %s", expr))
	lines = append(lines, "")
	lines = append(lines, "Fields:")
	for i, f := range fields {
		lines = append(lines, fmt.Sprintf("  %s: %s (range: %s)", fieldNames[i], explainField(f, fieldNames[i]), fieldRanges[i]))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Human readable: %s", cronToHuman(fields)))

	return strings.Join(lines, "\n"), nil
}

func (p *CronProfile) nextRuns(args map[string]interface{}) (string, error) {
	expr := getStr(args, "expression")
	if expr == "" {
		return "", fmt.Errorf("expression is required")
	}
	count := int(getFloat(args, "count"))
	if count <= 0 {
		count = 5
	}
	if count > 20 {
		count = 20
	}
	tz := getStr(args, "timezone")
	if tz == "" {
		tz = "UTC"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", fmt.Errorf("invalid timezone: %s", tz)
	}

	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return "", fmt.Errorf("cron expression must have 5 fields")
	}

	now := time.Now().In(loc)
	var runs []string
	candidate := now.Truncate(time.Minute).Add(time.Minute)

	for len(runs) < count && candidate.Before(now.Add(365*24*time.Hour)) {
		if matchesCron(candidate, fields) {
			runs = append(runs, candidate.Format("2006-01-02 15:04 (Mon)"))
		}
		candidate = candidate.Add(time.Minute)
	}

	if len(runs) == 0 {
		return fmt.Sprintf("No matching runs found for '%s' in the next year", expr), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Next %d runs for '%s' (%s):", len(runs), expr, tz))
	for i, r := range runs {
		lines = append(lines, fmt.Sprintf("  %d. %s", i+1, r))
	}
	return strings.Join(lines, "\n"), nil
}

func (p *CronProfile) cronBuilder(args map[string]interface{}) (string, error) {
	schedule := strings.ToLower(strings.TrimSpace(getStr(args, "schedule")))
	if schedule == "" {
		return "", fmt.Errorf("schedule is required")
	}

	var expr string
	switch {
	case schedule == "hourly":
		expr = "0 * * * *"
	case schedule == "midnight":
		expr = "0 0 * * *"
	case schedule == "noon":
		expr = "0 12 * * *"
	case strings.HasPrefix(schedule, "every ") && strings.HasSuffix(schedule, " minutes"):
		n := strings.TrimPrefix(strings.TrimSuffix(schedule, " minutes"), "every ")
		if _, err := strconv.Atoi(n); err != nil {
			return "", fmt.Errorf("invalid minute interval: %s", n)
		}
		expr = fmt.Sprintf("*/%s * * * *", n)
	case strings.HasPrefix(schedule, "every ") && strings.HasSuffix(schedule, " hours"):
		n := strings.TrimPrefix(strings.TrimSuffix(schedule, " hours"), "every ")
		if _, err := strconv.Atoi(n); err != nil {
			return "", fmt.Errorf("invalid hour interval: %s", n)
		}
		expr = fmt.Sprintf("0 */%s * * *", n)
	case strings.HasPrefix(schedule, "daily at "):
		timeStr := strings.TrimPrefix(schedule, "daily at ")
		h, m, err := parseTime(timeStr)
		if err != nil {
			return "", err
		}
		expr = fmt.Sprintf("%d %d * * *", m, h)
	case strings.HasPrefix(schedule, "weekly on "):
		rest := strings.TrimPrefix(schedule, "weekly on ")
		parts := strings.SplitN(rest, " at ", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("use format: weekly on DAY at HH:MM")
		}
		dow := dayToNum(parts[0])
		if dow < 0 {
			return "", fmt.Errorf("invalid day: %s", parts[0])
		}
		h, m, err := parseTime(parts[1])
		if err != nil {
			return "", err
		}
		expr = fmt.Sprintf("%d %d * * %d", m, h, dow)
	case strings.HasPrefix(schedule, "monthly on "):
		rest := strings.TrimPrefix(schedule, "monthly on ")
		parts := strings.SplitN(rest, " at ", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("use format: monthly on DAY at HH:MM")
		}
		day, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || day < 1 || day > 31 {
			return "", fmt.Errorf("invalid day of month: %s", parts[0])
		}
		h, m, err := parseTime(parts[1])
		if err != nil {
			return "", err
		}
		expr = fmt.Sprintf("%d %d %d * *", m, h, day)
	default:
		return "", fmt.Errorf("unrecognized schedule. Supported: 'every N minutes', 'every N hours', 'daily at HH:MM', 'weekly on DAY at HH:MM', 'monthly on DAY at HH:MM', 'hourly', 'midnight', 'noon'")
	}

	return fmt.Sprintf("Schedule: %s\nCron Expression: %s\nHuman readable: %s", schedule, expr, cronToHuman(strings.Fields(expr))), nil
}

func parseTime(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time format: use HH:MM")
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, 0, fmt.Errorf("invalid hour: %s", parts[0])
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid minute: %s", parts[1])
	}
	return h, m, nil
}

func dayToNum(day string) int {
	days := map[string]int{
		"sunday": 0, "sun": 0,
		"monday": 1, "mon": 1,
		"tuesday": 2, "tue": 2,
		"wednesday": 3, "wed": 3,
		"thursday": 4, "thu": 4,
		"friday": 5, "fri": 5,
		"saturday": 6, "sat": 6,
	}
	if n, ok := days[strings.ToLower(strings.TrimSpace(day))]; ok {
		return n
	}
	return -1
}

func explainField(field, name string) string {
	if field == "*" {
		return "every " + strings.ToLower(name)
	}
	if strings.HasPrefix(field, "*/") {
		return fmt.Sprintf("every %s", field[2:])
	}
	if strings.Contains(field, ",") {
		return fmt.Sprintf("at %s", field)
	}
	if strings.Contains(field, "-") {
		return fmt.Sprintf("from %s", field)
	}
	return field
}

func cronToHuman(fields []string) string {
	min, hour, dom, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]

	if min == "0" && hour == "0" && dom == "*" && month == "*" && dow == "*" {
		return "Every day at midnight"
	}
	if min == "0" && hour == "12" && dom == "*" && month == "*" && dow == "*" {
		return "Every day at noon"
	}
	if min == "0" && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		return "Every hour"
	}
	if strings.HasPrefix(min, "*/") && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		return fmt.Sprintf("Every %s minutes", min[2:])
	}
	if min != "*" && hour != "*" && dom == "*" && month == "*" && dow == "*" {
		return fmt.Sprintf("Daily at %s:%s", zeroPad(hour), zeroPad(min))
	}
	if min != "*" && hour != "*" && dom == "*" && month == "*" && dow != "*" {
		return fmt.Sprintf("Weekly on %s at %s:%s", dowName(dow), zeroPad(hour), zeroPad(min))
	}
	if min != "*" && hour != "*" && dom != "*" && month == "*" && dow == "*" {
		return fmt.Sprintf("Monthly on day %s at %s:%s", dom, zeroPad(hour), zeroPad(min))
	}
	return strings.Join(fields, " ")
}

func zeroPad(s string) string {
	if len(s) == 1 {
		return "0" + s
	}
	return s
}

func dowName(s string) string {
	names := map[string]string{"0": "Sunday", "1": "Monday", "2": "Tuesday", "3": "Wednesday", "4": "Thursday", "5": "Friday", "6": "Saturday"}
	if name, ok := names[s]; ok {
		return name
	}
	return s
}

func matchesCron(t time.Time, fields []string) bool {
	return matchField(fields[0], t.Minute(), 0, 59) &&
		matchField(fields[1], t.Hour(), 0, 23) &&
		matchField(fields[2], t.Day(), 1, 31) &&
		matchField(fields[3], int(t.Month()), 1, 12) &&
		matchField(fields[4], int(t.Weekday()), 0, 6)
}

func matchField(field string, value, min, max int) bool {
	if field == "*" {
		return true
	}
	if strings.HasPrefix(field, "*/") {
		step, err := strconv.Atoi(field[2:])
		if err != nil || step == 0 {
			return false
		}
		return value%step == 0
	}
	for _, part := range strings.Split(field, ",") {
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			lo, _ := strconv.Atoi(bounds[0])
			hi, _ := strconv.Atoi(bounds[1])
			if value >= lo && value <= hi {
				return true
			}
		} else {
			n, _ := strconv.Atoi(part)
			if n == value {
				return true
			}
		}
	}
	return false
}
