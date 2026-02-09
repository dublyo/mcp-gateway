package profiles

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type MathProfile struct{}

func (p *MathProfile) ID() string { return "math" }

func (p *MathProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "calculate",
			Description: "Evaluate a mathematical expression. Supports: +, -, *, /, %, ^, sqrt(), abs(), ceil(), floor(), round(), log(), log2(), log10(), sin(), cos(), tan(), pi, e",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"expression": map[string]interface{}{"type": "string", "description": "Math expression to evaluate"},
				},
				"required": []string{"expression"},
			},
		},
		{
			Name:        "statistics",
			Description: "Calculate statistics for a set of numbers (mean, median, mode, std dev, min, max, sum)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"numbers": map[string]interface{}{
						"type":        "string",
						"description": "Comma-separated list of numbers",
					},
				},
				"required": []string{"numbers"},
			},
		},
		{
			Name:        "convert_units",
			Description: "Convert between common units (length, weight, temperature, data, time)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"value": map[string]interface{}{"type": "number", "description": "Value to convert"},
					"from":  map[string]interface{}{"type": "string", "description": "Source unit (e.g. km, mi, kg, lb, C, F, GB, MB, hours, minutes)"},
					"to":    map[string]interface{}{"type": "string", "description": "Target unit"},
				},
				"required": []string{"value", "from", "to"},
			},
		},
		{
			Name:        "percentage",
			Description: "Calculate percentages: 'what is X% of Y', 'X is what % of Y', 'change from X to Y'",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type":        "string",
						"description": "Operation: 'of' (X% of Y), 'is' (X is what % of Y), 'change' (% change from X to Y)",
					},
					"x": map[string]interface{}{"type": "number", "description": "First value"},
					"y": map[string]interface{}{"type": "number", "description": "Second value"},
				},
				"required": []string{"operation", "x", "y"},
			},
		},
		{
			Name:        "number_base",
			Description: "Convert numbers between bases (binary, octal, decimal, hex)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"number":    map[string]interface{}{"type": "string", "description": "Number to convert (prefix with 0b, 0o, 0x for non-decimal)"},
					"from_base": map[string]interface{}{"type": "integer", "description": "Source base (2, 8, 10, 16). Default 10"},
					"to_base":   map[string]interface{}{"type": "integer", "description": "Target base (2, 8, 10, 16). Default shows all"},
				},
			},
		},
	}
}

func (p *MathProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "calculate":
		return p.calculate(args)
	case "statistics":
		return p.statistics(args)
	case "convert_units":
		return p.convertUnits(args)
	case "percentage":
		return p.percentage(args)
	case "number_base":
		return p.numberBase(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *MathProfile) calculate(args map[string]interface{}) (string, error) {
	expr := getStr(args, "expression")
	if expr == "" {
		return "", fmt.Errorf("expression is required")
	}
	result, err := evalExpr(expr)
	if err != nil {
		return "", err
	}
	if result == math.Trunc(result) && !math.IsInf(result, 0) {
		return fmt.Sprintf("%s = %d", expr, int64(result)), nil
	}
	return fmt.Sprintf("%s = %g", expr, result), nil
}

func (p *MathProfile) statistics(args map[string]interface{}) (string, error) {
	numStr := getStr(args, "numbers")
	if numStr == "" {
		return "", fmt.Errorf("numbers is required")
	}
	parts := strings.Split(numStr, ",")
	var nums []float64
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return "", fmt.Errorf("invalid number: %s", s)
		}
		nums = append(nums, n)
	}
	if len(nums) == 0 {
		return "", fmt.Errorf("no valid numbers provided")
	}

	sort.Float64s(nums)
	sum := 0.0
	for _, n := range nums {
		sum += n
	}
	mean := sum / float64(len(nums))

	var median float64
	n := len(nums)
	if n%2 == 0 {
		median = (nums[n/2-1] + nums[n/2]) / 2
	} else {
		median = nums[n/2]
	}

	// Standard deviation
	variance := 0.0
	for _, num := range nums {
		variance += (num - mean) * (num - mean)
	}
	stdDev := math.Sqrt(variance / float64(len(nums)))

	// Mode
	freq := map[float64]int{}
	maxFreq := 0
	for _, num := range nums {
		freq[num]++
		if freq[num] > maxFreq {
			maxFreq = freq[num]
		}
	}
	var modes []string
	for num, f := range freq {
		if f == maxFreq && maxFreq > 1 {
			modes = append(modes, fmt.Sprintf("%g", num))
		}
	}

	modeStr := "none"
	if len(modes) > 0 {
		modeStr = strings.Join(modes, ", ")
	}

	return fmt.Sprintf("Statistics for %d numbers:\n\nCount: %d\nSum: %g\nMean: %g\nMedian: %g\nMode: %s\nStd Dev: %g\nVariance: %g\nMin: %g\nMax: %g\nRange: %g",
		len(nums), len(nums), sum, mean, median, modeStr, stdDev, variance,
		nums[0], nums[len(nums)-1], nums[len(nums)-1]-nums[0]), nil
}

func (p *MathProfile) convertUnits(args map[string]interface{}) (string, error) {
	value := getFloat(args, "value")
	from := strings.ToLower(getStr(args, "from"))
	to := strings.ToLower(getStr(args, "to"))
	if from == "" || to == "" {
		return "", fmt.Errorf("from and to units are required")
	}

	// Normalize unit names
	unitMap := map[string]string{
		"kilometers": "km", "meters": "m", "centimeters": "cm", "millimeters": "mm",
		"miles": "mi", "yards": "yd", "feet": "ft", "inches": "in",
		"kilograms": "kg", "grams": "g", "milligrams": "mg", "pounds": "lb", "ounces": "oz",
		"celsius": "c", "fahrenheit": "f", "kelvin": "k",
		"gigabytes": "gb", "megabytes": "mb", "kilobytes": "kb", "bytes": "b", "terabytes": "tb",
		"hours": "h", "minutes": "min", "seconds": "s", "days": "d", "weeks": "w",
		"liters": "l", "milliliters": "ml", "gallons": "gal",
	}
	if mapped, ok := unitMap[from]; ok {
		from = mapped
	}
	if mapped, ok := unitMap[to]; ok {
		to = mapped
	}

	// Length -> meters
	lengthToMeters := map[string]float64{"km": 1000, "m": 1, "cm": 0.01, "mm": 0.001, "mi": 1609.344, "yd": 0.9144, "ft": 0.3048, "in": 0.0254}
	// Weight -> grams
	weightToGrams := map[string]float64{"kg": 1000, "g": 1, "mg": 0.001, "lb": 453.592, "oz": 28.3495}
	// Data -> bytes
	dataToBytes := map[string]float64{"tb": 1e12, "gb": 1e9, "mb": 1e6, "kb": 1e3, "b": 1}
	// Time -> seconds
	timeToSeconds := map[string]float64{"w": 604800, "d": 86400, "h": 3600, "min": 60, "s": 1}
	// Volume -> liters
	volumeToLiters := map[string]float64{"l": 1, "ml": 0.001, "gal": 3.78541}

	// Temperature special case
	if (from == "c" || from == "f" || from == "k") && (to == "c" || to == "f" || to == "k") {
		result := convertTemp(value, from, to)
		return fmt.Sprintf("%g %s = %g %s", value, strings.ToUpper(from), result, strings.ToUpper(to)), nil
	}

	conversionSets := []map[string]float64{lengthToMeters, weightToGrams, dataToBytes, timeToSeconds, volumeToLiters}
	for _, conv := range conversionSets {
		fromFactor, fromOk := conv[from]
		toFactor, toOk := conv[to]
		if fromOk && toOk {
			result := value * fromFactor / toFactor
			return fmt.Sprintf("%g %s = %g %s", value, from, result, to), nil
		}
	}

	return "", fmt.Errorf("cannot convert from %s to %s (unsupported or incompatible units)", from, to)
}

func convertTemp(value float64, from, to string) float64 {
	// Convert to Celsius first
	var celsius float64
	switch from {
	case "c":
		celsius = value
	case "f":
		celsius = (value - 32) * 5 / 9
	case "k":
		celsius = value - 273.15
	}
	switch to {
	case "c":
		return celsius
	case "f":
		return celsius*9/5 + 32
	case "k":
		return celsius + 273.15
	}
	return celsius
}

func (p *MathProfile) percentage(args map[string]interface{}) (string, error) {
	op := getStr(args, "operation")
	x := getFloat(args, "x")
	y := getFloat(args, "y")

	switch op {
	case "of":
		result := x / 100 * y
		return fmt.Sprintf("%g%% of %g = %g", x, y, result), nil
	case "is":
		if y == 0 {
			return "", fmt.Errorf("cannot divide by zero")
		}
		result := x / y * 100
		return fmt.Sprintf("%g is %g%% of %g", x, result, y), nil
	case "change":
		if x == 0 {
			return "", fmt.Errorf("cannot calculate change from zero")
		}
		result := (y - x) / x * 100
		direction := "increase"
		if result < 0 {
			direction = "decrease"
		}
		return fmt.Sprintf("Change from %g to %g: %g%% %s", x, y, math.Abs(result), direction), nil
	default:
		return "", fmt.Errorf("unknown operation: %s (use 'of', 'is', or 'change')", op)
	}
}

func (p *MathProfile) numberBase(args map[string]interface{}) (string, error) {
	numStr := getStr(args, "number")
	if numStr == "" {
		return "", fmt.Errorf("number is required")
	}

	fromBase := int(getFloat(args, "from_base"))
	if fromBase == 0 {
		fromBase = 10
	}

	// Parse the number
	numStr = strings.TrimSpace(numStr)
	if strings.HasPrefix(numStr, "0b") {
		numStr = numStr[2:]
		fromBase = 2
	} else if strings.HasPrefix(numStr, "0o") {
		numStr = numStr[2:]
		fromBase = 8
	} else if strings.HasPrefix(numStr, "0x") {
		numStr = numStr[2:]
		fromBase = 16
	}

	value, err := strconv.ParseInt(numStr, fromBase, 64)
	if err != nil {
		return "", fmt.Errorf("invalid number '%s' in base %d: %s", numStr, fromBase, err)
	}

	toBase := int(getFloat(args, "to_base"))
	if toBase != 0 {
		result := strconv.FormatInt(value, toBase)
		return fmt.Sprintf("%s (base %d) = %s (base %d)", numStr, fromBase, result, toBase), nil
	}

	return fmt.Sprintf("Number: %s (base %d)\n\nBinary:  %s\nOctal:   %s\nDecimal: %d\nHex:     %s",
		numStr, fromBase,
		strconv.FormatInt(value, 2),
		strconv.FormatInt(value, 8),
		value,
		strconv.FormatInt(value, 16)), nil
}

// Simple recursive descent expression evaluator
func evalExpr(expr string) (float64, error) {
	expr = strings.TrimSpace(expr)
	expr = strings.ReplaceAll(expr, "pi", fmt.Sprintf("%g", math.Pi))
	expr = strings.ReplaceAll(expr, "PI", fmt.Sprintf("%g", math.Pi))
	expr = strings.ReplaceAll(expr, " e ", fmt.Sprintf(" %g ", math.E))

	p := &exprParser{input: expr, pos: 0}
	result := p.parseExpression()
	if p.err != nil {
		return 0, p.err
	}
	return result, nil
}

type exprParser struct {
	input string
	pos   int
	err   error
}

func (p *exprParser) skipSpaces() {
	for p.pos < len(p.input) && p.input[p.pos] == ' ' {
		p.pos++
	}
}

func (p *exprParser) parseExpression() float64 {
	return p.parseAddSub()
}

func (p *exprParser) parseAddSub() float64 {
	left := p.parseMulDiv()
	for p.pos < len(p.input) {
		p.skipSpaces()
		if p.pos >= len(p.input) {
			break
		}
		op := p.input[p.pos]
		if op != '+' && op != '-' {
			break
		}
		p.pos++
		right := p.parseMulDiv()
		if op == '+' {
			left += right
		} else {
			left -= right
		}
	}
	return left
}

func (p *exprParser) parseMulDiv() float64 {
	left := p.parsePower()
	for p.pos < len(p.input) {
		p.skipSpaces()
		if p.pos >= len(p.input) {
			break
		}
		op := p.input[p.pos]
		if op != '*' && op != '/' && op != '%' {
			break
		}
		p.pos++
		right := p.parsePower()
		switch op {
		case '*':
			left *= right
		case '/':
			if right == 0 {
				p.err = fmt.Errorf("division by zero")
				return 0
			}
			left /= right
		case '%':
			left = math.Mod(left, right)
		}
	}
	return left
}

func (p *exprParser) parsePower() float64 {
	base := p.parseUnary()
	p.skipSpaces()
	if p.pos < len(p.input) && p.input[p.pos] == '^' {
		p.pos++
		exp := p.parseUnary()
		return math.Pow(base, exp)
	}
	return base
}

func (p *exprParser) parseUnary() float64 {
	p.skipSpaces()
	if p.pos < len(p.input) && p.input[p.pos] == '-' {
		p.pos++
		return -p.parseAtom()
	}
	if p.pos < len(p.input) && p.input[p.pos] == '+' {
		p.pos++
	}
	return p.parseAtom()
}

func (p *exprParser) parseAtom() float64 {
	p.skipSpaces()
	if p.pos >= len(p.input) {
		p.err = fmt.Errorf("unexpected end of expression")
		return 0
	}

	// Parentheses
	if p.input[p.pos] == '(' {
		p.pos++
		result := p.parseExpression()
		p.skipSpaces()
		if p.pos < len(p.input) && p.input[p.pos] == ')' {
			p.pos++
		}
		return result
	}

	// Functions
	funcs := []string{"sqrt", "abs", "ceil", "floor", "round", "log2", "log10", "log", "sin", "cos", "tan"}
	for _, fn := range funcs {
		if p.pos+len(fn) <= len(p.input) && strings.ToLower(p.input[p.pos:p.pos+len(fn)]) == fn {
			p.pos += len(fn)
			p.skipSpaces()
			if p.pos < len(p.input) && p.input[p.pos] == '(' {
				p.pos++
				arg := p.parseExpression()
				p.skipSpaces()
				if p.pos < len(p.input) && p.input[p.pos] == ')' {
					p.pos++
				}
				return applyFunc(fn, arg)
			}
		}
	}

	// Number
	start := p.pos
	if p.pos < len(p.input) && (p.input[p.pos] == '.' || (p.input[p.pos] >= '0' && p.input[p.pos] <= '9')) {
		for p.pos < len(p.input) && ((p.input[p.pos] >= '0' && p.input[p.pos] <= '9') || p.input[p.pos] == '.') {
			p.pos++
		}
		// Handle scientific notation
		if p.pos < len(p.input) && (p.input[p.pos] == 'e' || p.input[p.pos] == 'E') {
			p.pos++
			if p.pos < len(p.input) && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
				p.pos++
			}
			for p.pos < len(p.input) && p.input[p.pos] >= '0' && p.input[p.pos] <= '9' {
				p.pos++
			}
		}
		val, err := strconv.ParseFloat(p.input[start:p.pos], 64)
		if err != nil {
			p.err = fmt.Errorf("invalid number: %s", p.input[start:p.pos])
			return 0
		}
		return val
	}

	p.err = fmt.Errorf("unexpected character at position %d: '%c'", p.pos, p.input[p.pos])
	return 0
}

func applyFunc(name string, arg float64) float64 {
	switch name {
	case "sqrt":
		return math.Sqrt(arg)
	case "abs":
		return math.Abs(arg)
	case "ceil":
		return math.Ceil(arg)
	case "floor":
		return math.Floor(arg)
	case "round":
		return math.Round(arg)
	case "log":
		return math.Log(arg)
	case "log2":
		return math.Log2(arg)
	case "log10":
		return math.Log10(arg)
	case "sin":
		return math.Sin(arg)
	case "cos":
		return math.Cos(arg)
	case "tan":
		return math.Tan(arg)
	}
	return arg
}
