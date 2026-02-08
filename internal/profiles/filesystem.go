package profiles

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FilesystemProfile struct{}

func (p *FilesystemProfile) ID() string { return "filesystem" }

func (p *FilesystemProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "read_file",
			Description: "Read the contents of a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "File path to read"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "write_file",
			Description: "Write content to a file (creates or overwrites)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":    map[string]interface{}{"type": "string", "description": "File path to write to"},
					"content": map[string]interface{}{"type": "string", "description": "Content to write"},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "list_directory",
			Description: "List files and directories in a path",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "Directory path to list"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "search_files",
			Description: "Search for files by name pattern",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path":    map[string]interface{}{"type": "string", "description": "Directory to search in"},
					"pattern": map[string]interface{}{"type": "string", "description": "Glob pattern (e.g. *.txt)"},
				},
				"required": []string{"path", "pattern"},
			},
		},
		{
			Name:        "get_file_info",
			Description: "Get file metadata (size, modified date, permissions)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "File path"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "create_directory",
			Description: "Create a new directory (including parents)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{"type": "string", "description": "Directory path to create"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "move_file",
			Description: "Move or rename a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"source":      map[string]interface{}{"type": "string", "description": "Source file path"},
					"destination": map[string]interface{}{"type": "string", "description": "Destination file path"},
				},
				"required": []string{"source", "destination"},
			},
		},
		{
			Name:        "read_multiple_files",
			Description: "Read multiple files at once. Returns contents of each file.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"paths": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Array of file paths to read",
					},
				},
				"required": []string{"paths"},
			},
		},
	}
}

func (p *FilesystemProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	allowed := parseAllowedPaths(env["ALLOWED_PATHS"])

	switch name {
	case "read_file":
		path := getStr(args, "path")
		if err := validatePath(path, allowed); err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("cannot read file: %s", err)
		}
		return string(data), nil

	case "write_file":
		path := getStr(args, "path")
		content := getStr(args, "content")
		if err := validatePath(path, allowed); err != nil {
			return "", err
		}
		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return "", fmt.Errorf("cannot create parent directory: %s", err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("cannot write file: %s", err)
		}
		return fmt.Sprintf("Written %d bytes to %s", len(content), path), nil

	case "list_directory":
		path := getStr(args, "path")
		if err := validatePath(path, allowed); err != nil {
			return "", err
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return "", fmt.Errorf("cannot list directory: %s", err)
		}
		var lines []string
		for _, e := range entries {
			info, _ := e.Info()
			prefix := "📄"
			size := ""
			if e.IsDir() {
				prefix = "📁"
			} else if info != nil {
				size = fmt.Sprintf(" (%d bytes)", info.Size())
			}
			lines = append(lines, fmt.Sprintf("%s %s%s", prefix, e.Name(), size))
		}
		if len(lines) == 0 {
			return "Directory is empty", nil
		}
		return strings.Join(lines, "\n"), nil

	case "search_files":
		path := getStr(args, "path")
		pattern := getStr(args, "pattern")
		if err := validatePath(path, allowed); err != nil {
			return "", err
		}
		var matches []string
		filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if matched, _ := filepath.Match(pattern, info.Name()); matched {
				matches = append(matches, p)
			}
			if len(matches) > 100 {
				return fmt.Errorf("too many results")
			}
			return nil
		})
		if len(matches) == 0 {
			return "No files found matching pattern", nil
		}
		return fmt.Sprintf("Found %d files:\n%s", len(matches), strings.Join(matches, "\n")), nil

	case "get_file_info":
		path := getStr(args, "path")
		if err := validatePath(path, allowed); err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("cannot stat file: %s", err)
		}
		return fmt.Sprintf("Name: %s\nSize: %d bytes\nMode: %s\nModified: %s\nIsDir: %v",
			info.Name(), info.Size(), info.Mode(), info.ModTime().Format("2006-01-02 15:04:05"), info.IsDir()), nil

	case "create_directory":
		path := getStr(args, "path")
		if err := validatePath(path, allowed); err != nil {
			return "", err
		}
		if err := os.MkdirAll(path, 0755); err != nil {
			return "", fmt.Errorf("cannot create directory: %s", err)
		}
		return fmt.Sprintf("Created directory: %s", path), nil

	case "move_file":
		src := getStr(args, "source")
		dst := getStr(args, "destination")
		if err := validatePath(src, allowed); err != nil {
			return "", err
		}
		if err := validatePath(dst, allowed); err != nil {
			return "", err
		}
		if err := os.Rename(src, dst); err != nil {
			return "", fmt.Errorf("cannot move file: %s", err)
		}
		return fmt.Sprintf("Moved %s -> %s", src, dst), nil

	case "read_multiple_files":
		pathsRaw, ok := args["paths"]
		if !ok {
			return "", fmt.Errorf("paths is required")
		}
		pathsJSON, _ := json.Marshal(pathsRaw)
		var paths []string
		json.Unmarshal(pathsJSON, &paths)
		var results []string
		for _, path := range paths {
			if err := validatePath(path, allowed); err != nil {
				results = append(results, fmt.Sprintf("--- %s ---\nError: %s", path, err))
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				results = append(results, fmt.Sprintf("--- %s ---\nError: %s", path, err))
				continue
			}
			results = append(results, fmt.Sprintf("--- %s ---\n%s", path, string(data)))
		}
		return strings.Join(results, "\n\n"), nil

	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func parseAllowedPaths(s string) []string {
	if s == "" {
		return nil
	}
	var paths []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			abs, err := filepath.Abs(p)
			if err == nil {
				paths = append(paths, abs)
			}
		}
	}
	return paths
}

func validatePath(path string, allowed []string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %s", err)
	}

	// Resolve symlinks to prevent traversal
	resolved, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		// Dir might not exist yet (for write operations), check parent
		resolved = abs
	} else {
		resolved = filepath.Join(resolved, filepath.Base(abs))
	}

	if len(allowed) == 0 {
		return nil // No restrictions if ALLOWED_PATHS not set
	}

	for _, a := range allowed {
		if strings.HasPrefix(resolved, a) || strings.HasPrefix(abs, a) {
			return nil
		}
	}
	return fmt.Errorf("path %s is outside allowed directories", path)
}
