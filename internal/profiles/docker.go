package profiles

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type DockerProfile struct{}

func (p *DockerProfile) ID() string { return "docker" }

func (p *DockerProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "docker_list",
			Description: "List all containers with status, image, ports, and uptime",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"all": map[string]interface{}{
						"type":        "boolean",
						"description": "Include stopped containers (default: false)",
					},
				},
			},
		},
		{
			Name:        "docker_logs",
			Description: "Get container logs (stdout/stderr)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"container": map[string]interface{}{"type": "string", "description": "Container ID or name"},
					"tail": map[string]interface{}{
						"type":        "integer",
						"description": "Number of lines from the end (default: 100)",
						"default":     100,
					},
				},
				"required": []string{"container"},
			},
		},
		{
			Name:        "docker_inspect",
			Description: "Get detailed container configuration, network, mounts, and state",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"container": map[string]interface{}{"type": "string", "description": "Container ID or name"},
				},
				"required": []string{"container"},
			},
		},
		{
			Name:        "docker_stats",
			Description: "Get live resource usage (CPU, memory, network, disk I/O) for containers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"container": map[string]interface{}{
						"type":        "string",
						"description": "Container ID or name (empty = all running containers)",
					},
				},
			},
		},
		{
			Name:        "docker_restart",
			Description: "Restart a container (requires READ_ONLY=false)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"container": map[string]interface{}{"type": "string", "description": "Container ID or name"},
				},
				"required": []string{"container"},
			},
		},
		{
			Name:        "docker_exec",
			Description: "Execute a command inside a running container (requires READ_ONLY=false)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"container": map[string]interface{}{"type": "string", "description": "Container ID or name"},
					"command":   map[string]interface{}{"type": "string", "description": "Command to execute (e.g. 'ls -la /app')"},
				},
				"required": []string{"container", "command"},
			},
		},
	}
}

func (p *DockerProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	dockerHost := env["DOCKER_HOST"]
	if dockerHost == "" {
		dockerHost = "unix:///var/run/docker.sock"
	}

	readOnly := strings.ToLower(env["READ_ONLY"]) != "false"

	switch name {
	case "docker_list":
		return p.dockerList(dockerHost, args)
	case "docker_logs":
		return p.dockerLogs(dockerHost, args)
	case "docker_inspect":
		return p.dockerInspect(dockerHost, args)
	case "docker_stats":
		return p.dockerStats(dockerHost, args)
	case "docker_restart":
		if readOnly {
			return "", fmt.Errorf("docker_restart requires READ_ONLY=false")
		}
		return p.dockerRestart(dockerHost, args)
	case "docker_exec":
		if readOnly {
			return "", fmt.Errorf("docker_exec requires READ_ONLY=false")
		}
		return p.dockerExec(dockerHost, args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// dockerAPI makes an HTTP request to the Docker socket API
func (p *DockerProfile) dockerAPI(dockerHost, method, path string, body io.Reader) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	if strings.HasPrefix(dockerHost, "unix://") {
		socketPath := strings.TrimPrefix(dockerHost, "unix://")
		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return net.DialTimeout("unix", socketPath, 10*time.Second)
			},
		}
		dockerHost = "http://localhost"
	} else if strings.HasPrefix(dockerHost, "tcp://") {
		dockerHost = "http://" + strings.TrimPrefix(dockerHost, "tcp://")
	}

	url := dockerHost + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %s", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker API error: %s", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %s", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("docker API %d: %s", resp.StatusCode, string(data))
	}
	return data, nil
}

func (p *DockerProfile) dockerList(dockerHost string, args map[string]interface{}) (string, error) {
	path := "/containers/json"
	all, _ := args["all"].(bool)
	if all {
		path += "?all=true"
	}

	data, err := p.dockerAPI(dockerHost, "GET", path, nil)
	if err != nil {
		return "", err
	}

	var containers []map[string]interface{}
	if err := json.Unmarshal(data, &containers); err != nil {
		return "", fmt.Errorf("failed to parse response: %s", err)
	}

	if len(containers) == 0 {
		return "No containers found", nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("%-12s %-30s %-20s %-15s %s", "ID", "NAME", "IMAGE", "STATE", "STATUS"))
	lines = append(lines, strings.Repeat("-", 100))

	for _, c := range containers {
		id := fmt.Sprintf("%v", c["Id"])
		if len(id) > 12 {
			id = id[:12]
		}

		name := ""
		if names, ok := c["Names"].([]interface{}); ok && len(names) > 0 {
			name = strings.TrimPrefix(fmt.Sprintf("%v", names[0]), "/")
		}
		if len(name) > 30 {
			name = name[:27] + "..."
		}

		img := fmt.Sprintf("%v", c["Image"])
		if len(img) > 20 {
			img = img[:17] + "..."
		}

		state := fmt.Sprintf("%v", c["State"])
		status := fmt.Sprintf("%v", c["Status"])

		lines = append(lines, fmt.Sprintf("%-12s %-30s %-20s %-15s %s", id, name, img, state, status))
	}

	return fmt.Sprintf("Containers (%d):\n\n%s", len(containers), strings.Join(lines, "\n")), nil
}

func (p *DockerProfile) dockerLogs(dockerHost string, args map[string]interface{}) (string, error) {
	container := getStr(args, "container")
	if container == "" {
		return "", fmt.Errorf("container is required")
	}
	if strings.ContainsAny(container, " ;|&$`/") {
		return "", fmt.Errorf("invalid container name")
	}

	tail := int(getFloat(args, "tail"))
	if tail <= 0 {
		tail = 100
	}
	if tail > 1000 {
		tail = 1000
	}

	path := fmt.Sprintf("/containers/%s/logs?stdout=true&stderr=true&tail=%d", container, tail)
	data, err := p.dockerAPI(dockerHost, "GET", path, nil)
	if err != nil {
		return "", err
	}

	// Docker log stream has 8-byte headers per line, strip them
	result := cleanDockerLogs(data)
	if result == "" {
		return "(no logs)", nil
	}
	return fmt.Sprintf("Logs for %s (last %d lines):\n\n%s", container, tail, result), nil
}

func (p *DockerProfile) dockerInspect(dockerHost string, args map[string]interface{}) (string, error) {
	container := getStr(args, "container")
	if container == "" {
		return "", fmt.Errorf("container is required")
	}
	if strings.ContainsAny(container, " ;|&$`/") {
		return "", fmt.Errorf("invalid container name")
	}

	data, err := p.dockerAPI(dockerHost, "GET", fmt.Sprintf("/containers/%s/json", container), nil)
	if err != nil {
		return "", err
	}

	var info map[string]interface{}
	if err := json.Unmarshal(data, &info); err != nil {
		return "", fmt.Errorf("failed to parse response: %s", err)
	}

	// Extract key fields for a readable summary
	var lines []string
	lines = append(lines, fmt.Sprintf("Container: %s", container))

	if name, ok := info["Name"].(string); ok {
		lines = append(lines, fmt.Sprintf("Name: %s", strings.TrimPrefix(name, "/")))
	}
	if state, ok := info["State"].(map[string]interface{}); ok {
		lines = append(lines, fmt.Sprintf("Status: %v", state["Status"]))
		lines = append(lines, fmt.Sprintf("Running: %v", state["Running"]))
		lines = append(lines, fmt.Sprintf("Started: %v", state["StartedAt"]))
	}
	if config, ok := info["Config"].(map[string]interface{}); ok {
		lines = append(lines, fmt.Sprintf("Image: %v", config["Image"]))
		if envs, ok := config["Env"].([]interface{}); ok {
			lines = append(lines, fmt.Sprintf("Env vars: %d", len(envs)))
		}
	}
	if netSettings, ok := info["NetworkSettings"].(map[string]interface{}); ok {
		if ports, ok := netSettings["Ports"].(map[string]interface{}); ok && len(ports) > 0 {
			var portList []string
			for port := range ports {
				portList = append(portList, port)
			}
			lines = append(lines, fmt.Sprintf("Ports: %s", strings.Join(portList, ", ")))
		}
	}
	if mounts, ok := info["Mounts"].([]interface{}); ok && len(mounts) > 0 {
		lines = append(lines, fmt.Sprintf("Mounts: %d", len(mounts)))
		for _, m := range mounts {
			if mount, ok := m.(map[string]interface{}); ok {
				lines = append(lines, fmt.Sprintf("  %v -> %v (%v)", mount["Source"], mount["Destination"], mount["Mode"]))
			}
		}
	}

	return strings.Join(lines, "\n"), nil
}

func (p *DockerProfile) dockerStats(dockerHost string, args map[string]interface{}) (string, error) {
	container := getStr(args, "container")

	if container != "" {
		if strings.ContainsAny(container, " ;|&$`/") {
			return "", fmt.Errorf("invalid container name")
		}
		// Single container stats
		path := fmt.Sprintf("/containers/%s/stats?stream=false", container)
		data, err := p.dockerAPI(dockerHost, "GET", path, nil)
		if err != nil {
			return "", err
		}
		return formatContainerStats(container, data)
	}

	// List all running containers, get stats for each
	listData, err := p.dockerAPI(dockerHost, "GET", "/containers/json", nil)
	if err != nil {
		return "", err
	}

	var containers []map[string]interface{}
	if err := json.Unmarshal(listData, &containers); err != nil {
		return "", fmt.Errorf("failed to parse containers: %s", err)
	}

	if len(containers) == 0 {
		return "No running containers", nil
	}

	var results []string
	for _, c := range containers {
		id := fmt.Sprintf("%v", c["Id"])
		if len(id) > 12 {
			id = id[:12]
		}
		name := ""
		if names, ok := c["Names"].([]interface{}); ok && len(names) > 0 {
			name = strings.TrimPrefix(fmt.Sprintf("%v", names[0]), "/")
		}

		path := fmt.Sprintf("/containers/%s/stats?stream=false", id)
		data, err := p.dockerAPI(dockerHost, "GET", path, nil)
		if err != nil {
			results = append(results, fmt.Sprintf("%s (%s): error - %s", name, id, err))
			continue
		}
		stat, err := formatContainerStats(name, data)
		if err != nil {
			results = append(results, fmt.Sprintf("%s (%s): error - %s", name, id, err))
			continue
		}
		results = append(results, stat)
	}

	return fmt.Sprintf("Stats for %d containers:\n\n%s", len(containers), strings.Join(results, "\n\n")), nil
}

func (p *DockerProfile) dockerRestart(dockerHost string, args map[string]interface{}) (string, error) {
	container := getStr(args, "container")
	if container == "" {
		return "", fmt.Errorf("container is required")
	}
	if strings.ContainsAny(container, " ;|&$`/") {
		return "", fmt.Errorf("invalid container name")
	}

	_, err := p.dockerAPI(dockerHost, "POST", fmt.Sprintf("/containers/%s/restart?t=10", container), nil)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Container %s restarted successfully", container), nil
}

func (p *DockerProfile) dockerExec(dockerHost string, args map[string]interface{}) (string, error) {
	container := getStr(args, "container")
	if container == "" {
		return "", fmt.Errorf("container is required")
	}
	if strings.ContainsAny(container, " ;|&$`/") {
		return "", fmt.Errorf("invalid container name")
	}

	command := getStr(args, "command")
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	// Create exec instance
	cmdParts := strings.Fields(command)
	execConfig := map[string]interface{}{
		"AttachStdout": true,
		"AttachStderr": true,
		"Cmd":          cmdParts,
	}
	configJSON, _ := json.Marshal(execConfig)

	data, err := p.dockerAPI(dockerHost, "POST", fmt.Sprintf("/containers/%s/exec", container), strings.NewReader(string(configJSON)))
	if err != nil {
		return "", err
	}

	var execResp map[string]interface{}
	if err := json.Unmarshal(data, &execResp); err != nil {
		return "", fmt.Errorf("failed to create exec: %s", err)
	}

	execID := fmt.Sprintf("%v", execResp["Id"])

	// Start exec
	startConfig, _ := json.Marshal(map[string]interface{}{"Detach": false, "Tty": false})
	output, err := p.dockerAPI(dockerHost, "POST", fmt.Sprintf("/exec/%s/start", execID), strings.NewReader(string(startConfig)))
	if err != nil {
		return "", err
	}

	result := cleanDockerLogs(output)
	if result == "" {
		result = "(no output)"
	}
	return fmt.Sprintf("Exec in %s: %s\n\n%s", container, command, result), nil
}

// cleanDockerLogs strips Docker stream headers (8-byte prefix per frame)
func cleanDockerLogs(data []byte) string {
	var lines []string
	for len(data) > 0 {
		if len(data) < 8 {
			// Remainder is plain text
			lines = append(lines, string(data))
			break
		}
		// Docker multiplexed stream: first byte is stream type, bytes 4-7 are size
		streamType := data[0]
		if streamType > 2 {
			// Not a docker stream header, treat as plain text
			lines = append(lines, string(data))
			break
		}
		size := int(data[4])<<24 | int(data[5])<<16 | int(data[6])<<8 | int(data[7])
		data = data[8:]
		if size > len(data) {
			size = len(data)
		}
		if size > 0 {
			lines = append(lines, string(data[:size]))
		}
		data = data[size:]
	}
	return strings.TrimSpace(strings.Join(lines, ""))
}

// formatContainerStats formats Docker stats JSON into readable text
func formatContainerStats(name string, data []byte) (string, error) {
	var stats map[string]interface{}
	if err := json.Unmarshal(data, &stats); err != nil {
		return "", fmt.Errorf("failed to parse stats: %s", err)
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("--- %s ---", name))

	// CPU
	if cpuStats, ok := stats["cpu_stats"].(map[string]interface{}); ok {
		if preCpu, ok := stats["precpu_stats"].(map[string]interface{}); ok {
			cpuDelta := getNestedFloat(cpuStats, "cpu_usage", "total_usage") - getNestedFloat(preCpu, "cpu_usage", "total_usage")
			systemDelta := getNestedFloat(cpuStats, "system_cpu_usage") - getNestedFloat(preCpu, "system_cpu_usage")
			if systemDelta > 0 {
				cpuPercent := (cpuDelta / systemDelta) * 100.0
				lines = append(lines, fmt.Sprintf("CPU: %.2f%%", cpuPercent))
			}
		}
	}

	// Memory
	if memStats, ok := stats["memory_stats"].(map[string]interface{}); ok {
		usage := getNestedFloat(memStats, "usage")
		limit := getNestedFloat(memStats, "limit")
		if limit > 0 {
			lines = append(lines, fmt.Sprintf("Memory: %s / %s (%.1f%%)",
				humanBytes(usage), humanBytes(limit), (usage/limit)*100))
		}
	}

	// Network
	if networks, ok := stats["networks"].(map[string]interface{}); ok {
		var totalRx, totalTx float64
		for _, v := range networks {
			if net, ok := v.(map[string]interface{}); ok {
				if rx, ok := net["rx_bytes"].(float64); ok {
					totalRx += rx
				}
				if tx, ok := net["tx_bytes"].(float64); ok {
					totalTx += tx
				}
			}
		}
		lines = append(lines, fmt.Sprintf("Network: %s rx / %s tx", humanBytes(totalRx), humanBytes(totalTx)))
	}

	return strings.Join(lines, "\n"), nil
}

func getNestedFloat(m map[string]interface{}, keys ...string) float64 {
	var current interface{} = m
	for _, key := range keys {
		obj, ok := current.(map[string]interface{})
		if !ok {
			return 0
		}
		current = obj[key]
	}
	f, ok := current.(float64)
	if !ok {
		return 0
	}
	return f
}

func humanBytes(b float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	for _, unit := range units {
		if b < 1024 {
			return fmt.Sprintf("%.1f%s", b, unit)
		}
		b /= 1024
	}
	return fmt.Sprintf("%.1f PB", b)
}
