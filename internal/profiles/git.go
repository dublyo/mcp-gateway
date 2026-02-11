package profiles

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type GitProfile struct{}

func (p *GitProfile) ID() string { return "git" }

func (p *GitProfile) Tools() []Tool {
	return []Tool{
		{
			Name:        "git_status",
			Description: "Show working tree status (modified, staged, untracked files)",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "git_log",
			Description: "Show commit history with hash, author, date, and message",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"max_entries": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of commits to show (default from MAX_LOG_ENTRIES env or 50)",
					},
					"branch": map[string]interface{}{
						"type":        "string",
						"description": "Branch name (default: current branch)",
					},
				},
			},
		},
		{
			Name:        "git_diff",
			Description: "Show changes between working tree and index, or between commits",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"ref": map[string]interface{}{
						"type":        "string",
						"description": "Commit ref or range (e.g. HEAD~1, main..feature). Default: unstaged changes",
					},
					"file": map[string]interface{}{
						"type":        "string",
						"description": "Limit diff to a specific file path",
					},
					"staged": map[string]interface{}{
						"type":        "boolean",
						"description": "Show staged (cached) changes instead of unstaged",
					},
				},
			},
		},
		{
			Name:        "git_blame",
			Description: "Show who last modified each line of a file",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file": map[string]interface{}{"type": "string", "description": "File path to blame (relative to repo root)"},
				},
				"required": []string{"file"},
			},
		},
		{
			Name:        "git_branches",
			Description: "List local and remote branches with current branch indicator",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"all": map[string]interface{}{
						"type":        "boolean",
						"description": "Include remote branches (default: false)",
					},
				},
			},
		},
		{
			Name:        "git_show",
			Description: "Show details of a specific commit (message, author, diff)",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"ref": map[string]interface{}{
						"type":        "string",
						"description": "Commit hash or reference (default: HEAD)",
					},
				},
			},
		},
	}
}

func (p *GitProfile) CallTool(name string, args map[string]interface{}, env map[string]string) (string, error) {
	repoPath := env["REPO_PATH"]
	if repoPath == "" {
		return "", fmt.Errorf("REPO_PATH environment variable is required")
	}

	// Validate repo path exists and is a git repo
	repoPath = filepath.Clean(repoPath)
	if _, err := os.Stat(filepath.Join(repoPath, ".git")); err != nil {
		return "", fmt.Errorf("not a git repository: %s", repoPath)
	}

	switch name {
	case "git_status":
		return p.runGit(repoPath, "status", "--porcelain=v2", "--branch")
	case "git_log":
		return p.gitLog(repoPath, args, env)
	case "git_diff":
		return p.gitDiff(repoPath, args)
	case "git_blame":
		return p.gitBlame(repoPath, args)
	case "git_branches":
		return p.gitBranches(repoPath, args)
	case "git_show":
		return p.gitShow(repoPath, args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

func (p *GitProfile) runGit(repoPath string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git error: %s\n%s", err, string(out))
	}
	result := strings.TrimSpace(string(out))
	if result == "" {
		return "(no output)", nil
	}
	// Truncate very long output
	if len(result) > 50000 {
		result = result[:50000] + "\n... (truncated)"
	}
	return result, nil
}

func (p *GitProfile) gitLog(repoPath string, args map[string]interface{}, env map[string]string) (string, error) {
	maxEntries := int(getFloat(args, "max_entries"))
	if maxEntries <= 0 {
		if v, err := strconv.Atoi(env["MAX_LOG_ENTRIES"]); err == nil && v > 0 {
			maxEntries = v
		} else {
			maxEntries = 50
		}
	}
	if maxEntries > 500 {
		maxEntries = 500
	}

	gitArgs := []string{"log", fmt.Sprintf("-n%d", maxEntries), "--format=%H | %an | %ad | %s", "--date=short"}

	branch := getStr(args, "branch")
	if branch != "" {
		// Sanitize branch name
		if strings.ContainsAny(branch, " ;|&$`") {
			return "", fmt.Errorf("invalid branch name")
		}
		gitArgs = append(gitArgs, branch)
	}

	return p.runGit(repoPath, gitArgs...)
}

func (p *GitProfile) gitDiff(repoPath string, args map[string]interface{}) (string, error) {
	gitArgs := []string{"diff"}

	staged, _ := args["staged"].(bool)
	if staged {
		gitArgs = append(gitArgs, "--cached")
	}

	ref := getStr(args, "ref")
	if ref != "" {
		if strings.ContainsAny(ref, " ;|&$`") {
			return "", fmt.Errorf("invalid ref")
		}
		gitArgs = append(gitArgs, ref)
	}

	file := getStr(args, "file")
	if file != "" {
		if strings.Contains(file, "..") {
			return "", fmt.Errorf("invalid file path")
		}
		gitArgs = append(gitArgs, "--", file)
	}

	return p.runGit(repoPath, gitArgs...)
}

func (p *GitProfile) gitBlame(repoPath string, args map[string]interface{}) (string, error) {
	file := getStr(args, "file")
	if file == "" {
		return "", fmt.Errorf("file is required")
	}
	if strings.Contains(file, "..") {
		return "", fmt.Errorf("invalid file path")
	}
	return p.runGit(repoPath, "blame", "--date=short", file)
}

func (p *GitProfile) gitBranches(repoPath string, args map[string]interface{}) (string, error) {
	gitArgs := []string{"branch", "-v"}
	all, _ := args["all"].(bool)
	if all {
		gitArgs = append(gitArgs, "-a")
	}
	return p.runGit(repoPath, gitArgs...)
}

func (p *GitProfile) gitShow(repoPath string, args map[string]interface{}) (string, error) {
	ref := getStr(args, "ref")
	if ref == "" {
		ref = "HEAD"
	}
	if strings.ContainsAny(ref, " ;|&$`") {
		return "", fmt.Errorf("invalid ref")
	}
	return p.runGit(repoPath, "show", "--stat", "--format=Commit: %H%nAuthor: %an <%ae>%nDate:   %ad%n%n%s%n%n%b", ref)
}
