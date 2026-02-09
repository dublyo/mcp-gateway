package profiles

import (
	"bufio"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type RedisProfile struct{}

func (p *RedisProfile) ID() string { return "redis" }

func (p *RedisProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "redis_get",
			Description: "Get the value of a key",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]interface{}{"type": "string", "description": "Key to get"},
				},
				"required": []string{"key"},
			},
		},
		{
			Name:        "redis_set",
			Description: "Set a key-value pair with optional TTL",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key":   map[string]interface{}{"type": "string", "description": "Key to set"},
					"value": map[string]interface{}{"type": "string", "description": "Value to set"},
					"ttl":   map[string]interface{}{"type": "integer", "description": "TTL in seconds (optional, 0 = no expiry)"},
				},
				"required": []string{"key", "value"},
			},
		},
		{
			Name:        "redis_del",
			Description: "Delete one or more keys",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"keys": map[string]interface{}{"type": "string", "description": "Comma-separated list of keys to delete"},
				},
				"required": []string{"keys"},
			},
		},
		{
			Name:        "redis_keys",
			Description: "List keys matching a pattern",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pattern": map[string]interface{}{"type": "string", "description": "Pattern to match (e.g. 'user:*'). Default '*'"},
				},
			},
		},
		{
			Name:        "redis_info",
			Description: "Get Redis server information",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"section": map[string]interface{}{"type": "string", "description": "Info section: server, memory, stats, keyspace, all (default keyspace)"},
				},
			},
		},
		{
			Name:        "redis_ttl",
			Description: "Get the TTL (time to live) of a key in seconds",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"key": map[string]interface{}{"type": "string", "description": "Key to check TTL"},
				},
				"required": []string{"key"},
			},
		},
	}
}

func (p *RedisProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	switch name {
	case "redis_get":
		return p.redisCmd(env, "GET", getStr(args, "key"))
	case "redis_set":
		return p.redisSet(args, env)
	case "redis_del":
		return p.redisDel(args, env)
	case "redis_keys":
		return p.redisKeys(args, env)
	case "redis_info":
		return p.redisInfo(args, env)
	case "redis_ttl":
		return p.redisCmd(env, "TTL", getStr(args, "key"))
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *RedisProfile) redisSet(args map[string]interface{}, env map[string]string) (string, error) {
	key := getStr(args, "key")
	value := getStr(args, "value")
	if key == "" || value == "" {
		return "", fmt.Errorf("key and value are required")
	}
	ttl := int(getFloat(args, "ttl"))
	if ttl > 0 {
		return p.redisCmd(env, "SET", key, value, "EX", strconv.Itoa(ttl))
	}
	return p.redisCmd(env, "SET", key, value)
}

func (p *RedisProfile) redisDel(args map[string]interface{}, env map[string]string) (string, error) {
	keysStr := getStr(args, "keys")
	if keysStr == "" {
		return "", fmt.Errorf("keys is required")
	}
	keys := strings.Split(keysStr, ",")
	cmdArgs := make([]string, 0, len(keys)+1)
	cmdArgs = append(cmdArgs, "DEL")
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k != "" {
			cmdArgs = append(cmdArgs, k)
		}
	}
	return p.redisCmd(env, cmdArgs[0], cmdArgs[1:]...)
}

func (p *RedisProfile) redisKeys(args map[string]interface{}, env map[string]string) (string, error) {
	pattern := getStr(args, "pattern")
	if pattern == "" {
		pattern = "*"
	}
	maxKeys := 100
	if mk := env["MAX_KEYS"]; mk != "" {
		if n, err := strconv.Atoi(mk); err == nil && n > 0 {
			maxKeys = n
		}
	}

	// Use SCAN instead of KEYS for safety
	conn, err := p.connect(env)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	var allKeys []string
	cursor := "0"
	for {
		resp, err := sendCommand(conn, "SCAN", cursor, "MATCH", pattern, "COUNT", "100")
		if err != nil {
			return "", err
		}
		// SCAN returns [cursor, [keys...]]
		parts := strings.SplitN(resp, "\n", 2)
		if len(parts) < 2 {
			break
		}
		cursor = strings.TrimSpace(parts[0])
		keyList := strings.TrimSpace(parts[1])
		if keyList != "(empty)" && keyList != "" {
			for _, k := range strings.Split(keyList, "\n") {
				k = strings.TrimSpace(k)
				if k != "" {
					allKeys = append(allKeys, k)
				}
			}
		}
		if cursor == "0" || len(allKeys) >= maxKeys {
			break
		}
	}

	if len(allKeys) == 0 {
		return fmt.Sprintf("No keys matching '%s'", pattern), nil
	}
	if len(allKeys) > maxKeys {
		allKeys = allKeys[:maxKeys]
	}
	return fmt.Sprintf("Keys matching '%s' (%d):\n%s", pattern, len(allKeys), strings.Join(allKeys, "\n")), nil
}

func (p *RedisProfile) redisInfo(args map[string]interface{}, env map[string]string) (string, error) {
	section := getStr(args, "section")
	if section == "" {
		section = "keyspace"
	}
	if section == "all" {
		return p.redisCmd(env, "INFO")
	}
	return p.redisCmd(env, "INFO", section)
}

// Minimal RESP protocol client
func (p *RedisProfile) connect(env map[string]string) (net.Conn, error) {
	redisURL := env["REDIS_URL"]
	if redisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is not configured")
	}

	u, err := url.Parse(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid REDIS_URL: %s", err)
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":6379"
	}

	conn, err := net.DialTimeout("tcp", host, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %s", err)
	}

	// AUTH if password present
	if u.User != nil {
		pass, _ := u.User.Password()
		if pass != "" {
			_, err := sendCommand(conn, "AUTH", pass)
			if err != nil {
				conn.Close()
				return nil, fmt.Errorf("auth failed: %s", err)
			}
		}
	}

	// SELECT database if path present
	if u.Path != "" && u.Path != "/" {
		db := strings.TrimPrefix(u.Path, "/")
		if db != "" && db != "0" {
			_, err := sendCommand(conn, "SELECT", db)
			if err != nil {
				conn.Close()
				return nil, fmt.Errorf("select db failed: %s", err)
			}
		}
	}

	return conn, nil
}

func (p *RedisProfile) redisCmd(env map[string]string, cmd string, args ...string) (string, error) {
	conn, err := p.connect(env)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	resp, err := sendCommand(conn, cmd, args...)
	if err != nil {
		return "", err
	}
	return resp, nil
}

func sendCommand(conn net.Conn, cmd string, args ...string) (string, error) {
	// Build RESP array
	parts := append([]string{cmd}, args...)
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("*%d\r\n", len(parts)))
	for _, p := range parts {
		buf.WriteString(fmt.Sprintf("$%d\r\n%s\r\n", len(p), p))
	}

	conn.SetDeadline(time.Now().Add(10 * time.Second))
	_, err := conn.Write([]byte(buf.String()))
	if err != nil {
		return "", fmt.Errorf("write failed: %s", err)
	}

	reader := bufio.NewReader(conn)
	return readResp(reader)
}

func readResp(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("read failed: %s", err)
	}
	line = strings.TrimRight(line, "\r\n")

	if len(line) == 0 {
		return "", fmt.Errorf("empty response")
	}

	switch line[0] {
	case '+': // Simple string
		return line[1:], nil
	case '-': // Error
		return "", fmt.Errorf("redis error: %s", line[1:])
	case ':': // Integer
		return line[1:], nil
	case '$': // Bulk string
		length, _ := strconv.Atoi(line[1:])
		if length == -1 {
			return "(nil)", nil
		}
		data := make([]byte, length+2) // +2 for \r\n
		_, err := reader.Read(data)
		if err != nil {
			return "", fmt.Errorf("read bulk failed: %s", err)
		}
		return string(data[:length]), nil
	case '*': // Array
		count, _ := strconv.Atoi(line[1:])
		if count == -1 {
			return "(empty)", nil
		}
		var items []string
		for i := 0; i < count; i++ {
			item, err := readResp(reader)
			if err != nil {
				return "", err
			}
			items = append(items, item)
		}
		return strings.Join(items, "\n"), nil
	}
	return line, nil
}
