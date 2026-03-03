package tools

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"golang.org/x/term"

	"g-rump-cli/internal/changes"
	"g-rump-cli/internal/ollama"
)

// Definitions returns the tool schemas for the Ollama API.
func Definitions() []ollama.Tool {
	return []ollama.Tool{
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "read_file",
				Description: "Read the contents of a file at the given path. Returns the file contents as text with line numbers.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"path": {Type: "string", Description: "File path to read (absolute or relative to cwd)"},
					},
					Required: []string{"path"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "write_file",
				Description: "Create or overwrite a file with the given content. Creates parent directories as needed. Always read a file before overwriting it.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"path":    {Type: "string", Description: "File path to write"},
						"content": {Type: "string", Description: "Full content to write to the file"},
					},
					Required: []string{"path", "content"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "edit_file",
				Description: "Edit a file by finding an exact string and replacing it. The old_string must appear exactly once in the file. Always read the file first before editing.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"path":       {Type: "string", Description: "File path to edit"},
						"old_string": {Type: "string", Description: "Exact text to find in the file (must be unique)"},
						"new_string": {Type: "string", Description: "Replacement text"},
					},
					Required: []string{"path", "old_string", "new_string"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "bash",
				Description: "Execute a shell command and return stdout and stderr. Use for running builds, tests, git commands, installing packages, etc.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"command": {Type: "string", Description: "Bash command to execute"},
						"timeout": {Type: "string", Description: "Timeout in seconds (default: 120)"},
					},
					Required: []string{"command"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "glob",
				Description: "Find files matching a glob pattern. Use ** for recursive directory matching (e.g. '**/*.go' finds all Go files). Skips .git, node_modules, vendor, etc.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"pattern": {Type: "string", Description: "Glob pattern (e.g., '**/*.go', 'src/**/*.ts', '*.json')"},
						"path":    {Type: "string", Description: "Base directory to search from (default: current directory)"},
					},
					Required: []string{"pattern"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "grep",
				Description: "Search file contents using a regex pattern. Returns matching lines with file paths and line numbers. Supports context lines around matches.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"pattern": {Type: "string", Description: "Regex pattern to search for"},
						"path":    {Type: "string", Description: "Directory to search in (default: current directory)"},
						"include": {Type: "string", Description: "Glob to filter files (e.g., '*.go', '*.py')"},
						"before":  {Type: "string", Description: "Number of lines to show before each match (max 10)"},
						"after":   {Type: "string", Description: "Number of lines to show after each match (max 10)"},
					},
					Required: []string{"pattern"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "start_server",
				Description: "Start a long-running background process (like 'npm run dev' or 'python app.py'). It runs entirely in the background. Use this instead of 'bash' for dev servers so you don't get blocked.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"command": {Type: "string", Description: "The shell command to start the server"},
					},
					Required: []string{"command"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "list_servers",
				Description: "List all background servers currently running via start_server, including their status and IDs.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{},
					Required: []string{},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "read_server_logs",
				Description: "Read the stdout/stderr output of a background server you started.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"id": {Type: "string", Description: "The ID of the server to check"},
					},
					Required: []string{"id"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "stop_server",
				Description: "Kill a background server process.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"id": {Type: "string", Description: "The ID of the server to stop"},
					},
					Required: []string{"id"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "fetch_webpage",
				Description: "Fetch the HTML of ANY URL (e.g., http://localhost:3000, https://docs.python.org) and return a clean DOM snapshot. Use this to read documentation, check API endpoints, or visually VERIFY that your frontend components rendered correctly.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"url": {Type: "string", Description: "The URL to fetch"},
					},
					Required: []string{"url"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "search_web",
				Description: "Search the public internet using DuckDuckGo to find documentation, solutions to obscure errors, or learn about new frameworks.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"query": {Type: "string", Description: "The search query"},
					},
					Required: []string{"query"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "browser_action",
				Description: "Control a headless Chromium browser using Playwright to test UI, extract text, or verify behavior. Actions: 'goto' (needs url), 'click' (needs selector), 'fill' (needs selector, text), 'evaluate' (needs script), 'extract_text', 'close'.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"action":   {Type: "string", Description: "The action to perform: goto, click, fill, evaluate, extract_text, close"},
						"url":      {Type: "string", Description: "URL for goto action"},
						"selector": {Type: "string", Description: "CSS selector for click/fill actions"},
						"text":     {Type: "string", Description: "Text for fill action"},
						"script":   {Type: "string", Description: "JS script for evaluate action"},
					},
					Required: []string{"action"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "project_map",
				Description: "Generate a tree-like map of the directory structure to understand project architecture. Respects .gitignore.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"path": {Type: "string", Description: "The directory to map (default: '.')"},
						"depth": {Type: "string", Description: "Maximum depth to recurse (default: 3)"},
					},
					Required: []string{},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "delegate_task",
				Description: "SWARM CAPABILITY: Spawn an independent, isolated sub-agent in the background to execute a specific sub-task. It will run autonomously and return its final summary. Use this to parallelize complex architectural changes or offload tedious fixes.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"task": {Type: "string", Description: "The highly-detailed instruction for the sub-agent"},
					},
					Required: []string{"task"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "remember_project_fact",
				Description: "Write a newly discovered fact about this specific codebase into the localized `.bu/knowledge.json` graph. This gives the agent permanent local memory for this project. Categories: 'architecture', 'dependencies', 'rules', 'custom'.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"category": {Type: "string", Description: "The category (architecture, dependencies, rules, custom)"},
						"fact":     {Type: "string", Description: "The specific insight, path, or rule to remember for this project"},
					},
					Required: []string{"category", "fact"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "save_memory",
				Description: "Save a fact, preference, or architectural constraint to the user's global memory. This memory is persisted across all sessions and should be used to remember things the user wants you to always know.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"fact": {Type: "string", Description: "The specific fact or rule to remember globally"},
					},
					Required: []string{"fact"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "run_tests",
				Description: "Auto-detect the project's test framework and run tests. Supports Go, Node, Rust, Python, and Ruby projects.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"path":   {Type: "string", Description: "Directory to run tests in (default: current directory)"},
						"filter": {Type: "string", Description: "Optional test name filter (e.g., 'TestFoo' for Go, 'my-test' for npm)"},
					},
					Required: []string{},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "http_request",
				Description: "Make an HTTP request to any URL. Supports GET, POST, PUT, PATCH, DELETE, HEAD methods with custom headers and body. Useful for testing APIs.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"method":  {Type: "string", Description: "HTTP method: GET, POST, PUT, PATCH, DELETE, HEAD"},
						"url":     {Type: "string", Description: "The URL to request"},
						"headers": {Type: "string", Description: "Optional JSON object of headers (e.g., '{\"Authorization\": \"Bearer xxx\"}')"},
						"body":    {Type: "string", Description: "Optional request body"},
					},
					Required: []string{"method", "url"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "watch_files",
				Description: "Watch files for changes by polling modification times. Returns a summary of which files changed during the watch period.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"paths":    {Type: "string", Description: "Comma-separated file paths or glob patterns to watch"},
						"duration": {Type: "string", Description: "How long to watch in seconds (default: 10, max: 60)"},
					},
					Required: []string{"paths"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "batch_replace",
				Description: "Find and replace a string across multiple files. Walks the directory tree, skipping .git, node_modules, vendor, etc. Records changes for undo.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"old_string": {Type: "string", Description: "The exact string to find and replace"},
						"new_string": {Type: "string", Description: "The replacement string"},
						"include":    {Type: "string", Description: "Optional glob filter for files (e.g., '*.go', '*.ts')"},
						"path":       {Type: "string", Description: "Base directory to search from (default: '.')"},
					},
					Required: []string{"old_string", "new_string"},
				},
			},
		},
		{
			Type: "function",
			Function: ollama.ToolFunction{
				Name:        "list_directory",
				Description: "List files and directories in a given path. Shows type (file/dir), size, and modification time. Good for exploring project structure.",
				Parameters: ollama.ToolParams{
					Type: "object",
					Properties: map[string]ollama.ToolProperty{
						"path": {Type: "string", Description: "Directory path to list (default: current directory)"},
					},
					Required: []string{},
				},
			},
		},
	}
}

// AutoAllowed returns the set of tools that don't need user permission.
func AutoAllowed() map[string]bool {
	return map[string]bool{
		"read_file":        true,
		"glob":             true,
		"grep":             true,
		"list_directory":   true,
		"save_memory":           true,
		"remember_project_fact": true,
		"list_servers":          true,
		"read_server_logs": true,
		"http_request":     true,
		"watch_files":      true,
		"fetch_webpage":    true,
		"search_web":       true,
		"browser_action":   true,
		"project_map":      true,
		"batch_replace":    false, // Require permission before multi-file replace
		"run_tests":        false, // Require permission before running tests
		"delegate_task":    false, // Require permission before spinning up a whole sub-agent
	}
}

// Execute runs the named tool with the given arguments and returns the result.
// The tracker records file changes for undo support. It may be nil.
func Execute(name string, args map[string]interface{}, tracker *changes.Tracker) (string, error) {
	result, err := executeInner(name, args, tracker)
	if err != nil {
		return result, WrapToolError(name, err)
	}
	return result, nil
}

func executeInner(name string, args map[string]interface{}, tracker *changes.Tracker) (string, error) {
	switch name {
	case "read_file":
		return execReadFile(args)
	case "write_file":
		return execWriteFile(args, tracker)
	case "edit_file":
		return execEditFile(args, tracker)
	case "bash":
		return execBash(args)
	case "glob":
		return execGlob(args)
	case "grep":
		return execGrep(args)
	case "run_tests":
		return execRunTests(args)
	case "list_directory":
		return execListDirectory(args)
	case "save_memory":
		return ToolSaveMemory(args)
	case "remember_project_fact":
		return ToolRememberProjectFact(args)
	case "start_server":
		return ToolStartServer(args)
	case "list_servers":
		return ToolListServers(args)
	case "read_server_logs":
		return ToolReadServerLogs(args)
	case "stop_server":
		return ToolStopServer(args)
	case "fetch_webpage":
		return ToolFetchLocalURL(args)
	case "search_web":
		return ToolSearchWeb(args)
	case "browser_action":
		return ToolBrowserAction(args)
	case "watch_files":
		return execWatchFiles(args)
	case "batch_replace":
		return execBatchReplace(args, tracker)
	case "http_request":
		return execHttpRequest(args)
	case "project_map":
		return execProjectMap(args)
	case "delegate_task":
		return execDelegateTask(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// ValidateArgs checks tool arguments before execution, returning an error
// for invalid or missing required parameters.
func ValidateArgs(toolName string, args map[string]interface{}) error {
	requireStr := func(key string) error {
		v := str(args, key)
		if v == "" {
			return fmt.Errorf("%s: '%s' parameter is required and must be a non-empty string", toolName, key)
		}
		return nil
	}

	switch toolName {
	case "read_file":
		return requireStr("path")
	case "write_file":
		if err := requireStr("path"); err != nil {
			return err
		}
		return requireStr("content")
	case "edit_file":
		if err := requireStr("path"); err != nil {
			return err
		}
		if err := requireStr("old_string"); err != nil {
			return err
		}
		if err := requireStr("new_string"); err != nil {
			return err
		}
		if str(args, "old_string") == str(args, "new_string") {
			return fmt.Errorf("edit_file: old_string and new_string must be different")
		}
	case "bash":
		return requireStr("command")
	case "glob":
		return requireStr("pattern")
	case "grep":
		if err := requireStr("pattern"); err != nil {
			return err
		}
		// Validate before/after are valid integers 0-10
		for _, key := range []string{"before", "after"} {
			if v := str(args, key); v != "" {
				var n int
				if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n < 0 || n > 10 {
					return fmt.Errorf("grep: '%s' must be an integer between 0 and 10", key)
				}
			}
		}
	case "batch_replace":
		return requireStr("path")
	}

	return nil
}

// --- Tool Implementations ---

func execReadFile(args map[string]interface{}) (string, error) {
	path := str(args, "path")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	// Add line numbers
	var numbered []string
	limit := len(lines)
	truncated := false
	if limit > 2000 {
		limit = 2000
		truncated = true
	}
	for i := 0; i < limit; i++ {
		numbered = append(numbered, fmt.Sprintf("%4d│ %s", i+1, lines[i]))
	}

	result := strings.Join(numbered, "\n")
	if truncated {
		result += fmt.Sprintf("\n... (truncated — showing %d of %d lines)", limit, len(lines))
	}

	return result, nil
}

func execWriteFile(args map[string]interface{}, tracker *changes.Tracker) (string, error) {
	path := str(args, "path")
	content := str(args, "content")
	if path == "" {
		return "", fmt.Errorf("path is required")
	}

	// Record old content for undo
	if tracker != nil {
		oldContent := ""
		if data, err := os.ReadFile(path); err == nil {
			oldContent = string(data)
		}
		tracker.Record("write", path, oldContent)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("creating directories: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}

	lines := strings.Count(content, "\n") + 1
	return fmt.Sprintf("File written: %s (%d lines)", path, lines), nil
}

func execEditFile(args map[string]interface{}, tracker *changes.Tracker) (string, error) {
	path := str(args, "path")
	oldStr := str(args, "old_string")
	newStr := str(args, "new_string")

	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if oldStr == "" {
		return "", fmt.Errorf("old_string is required and cannot be empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	content := string(data)
	count := strings.Count(content, oldStr)

	if count == 0 {
		// Fuzzy whitespace-normalized fallback: collapse runs of whitespace to single spaces
		normalizeWS := func(s string) string {
			// Replace all runs of whitespace (spaces, tabs, newlines) with a single space
			parts := strings.Fields(s)
			return strings.Join(parts, " ")
		}
		normContent := normalizeWS(content)
		normOld := normalizeWS(oldStr)
		normCount := strings.Count(normContent, normOld)

		if normCount == 1 {
			// Find the corresponding region in the original content.
			// Walk through original content matching normalized tokens.
			normIdx := strings.Index(normContent, normOld)
			// Map normalized index back to original content position.
			origStart := -1
			origEnd := -1
			normPos := 0
			i := 0
			// Skip leading whitespace in content for normPos tracking
			for i < len(content) && normPos < normIdx {
				if content[i] == ' ' || content[i] == '\t' || content[i] == '\n' || content[i] == '\r' {
					// Skip whitespace in original; in normalized it becomes single space
					j := i
					for j < len(content) && (content[j] == ' ' || content[j] == '\t' || content[j] == '\n' || content[j] == '\r') {
						j++
					}
					if normPos > 0 || j < len(content) { // only count space if not leading
						normPos++ // the single space in normalized form
					}
					i = j
				} else {
					normPos++
					i++
				}
			}
			origStart = i

			// Now advance through normOld length
			normPos = 0
			for i < len(content) && normPos < len(normOld) {
				if content[i] == ' ' || content[i] == '\t' || content[i] == '\n' || content[i] == '\r' {
					j := i
					for j < len(content) && (content[j] == ' ' || content[j] == '\t' || content[j] == '\n' || content[j] == '\r') {
						j++
					}
					normPos++ // single space
					i = j
				} else {
					normPos++
					i++
				}
			}
			origEnd = i

			if origStart >= 0 && origEnd > origStart && origEnd <= len(content) {
				// Record old content for undo
				if tracker != nil {
					tracker.Record("edit", path, content)
				}
				newContent := content[:origStart] + newStr + content[origEnd:]
				if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
					return "", err
				}
				return fmt.Sprintf("File edited (fuzzy whitespace match): %s", path), nil
			}
		}

		if normCount > 1 {
			return "", fmt.Errorf("old_string found %d times via fuzzy match in %s (must be unique)", normCount, path)
		}
		return "", fmt.Errorf("old_string not found in %s", path)
	}
	if count > 1 {
		return "", fmt.Errorf("old_string found %d times in %s (must be unique — provide more context)", count, path)
	}

	// Record old content for undo
	if tracker != nil {
		tracker.Record("edit", path, content)
	}

	newContent := strings.Replace(content, oldStr, newStr, 1)
	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return "", err
	}

	return fmt.Sprintf("File edited: %s", path), nil
}

func execBash(args map[string]interface{}) (string, error) {
	command := str(args, "command")
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	timeout := envTimeout("GRUMP_BASH_TIMEOUT", 120)
	if t := str(args, "timeout"); t != "" {
		if d, err := time.ParseDuration(t + "s"); err == nil {
			timeout = d
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	setProcGroup(cmd)
	cmd.Cancel = cancelProcess(cmd)

	// Use pipes for progressive output
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = cmd.Stdout // merge stderr into stdout pipe

	if err := cmd.Start(); err != nil {
		return "", err
	}

	// Read output progressively
	var buf strings.Builder
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	start := time.Now()
	liveShown := false
	isTTY := term.IsTerminal(int(os.Stderr.Fd()))

	for scanner.Scan() {
		line := scanner.Text()
		buf.WriteString(line)
		buf.WriteByte('\n')

		// After 3 seconds, start showing live output on TTY
		if isTTY && time.Since(start) > 3*time.Second {
			if !liveShown {
				fmt.Fprintf(os.Stderr, "\033[2m") // dim
				liveShown = true
			}
			// Show last line as live progress (overwrite previous)
			display := line
			if len(display) > 120 {
				display = display[:120] + "..."
			}
			fmt.Fprintf(os.Stderr, "\r\033[K  %s", display)
		}
	}

	if liveShown {
		fmt.Fprintf(os.Stderr, "\r\033[K\033[0m") // clear line and reset
	}

	err := cmd.Wait()
	result := buf.String()

	if err != nil {
		if ctx.Err() != nil {
			return result + fmt.Sprintf("\n(command timed out after %s)", timeout), nil
		}
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return result + fmt.Sprintf("\nexit code: %d", exitCode), nil
	}

	// Truncate very long output
	if len(result) > 50000 {
		result = result[:50000] + "\n... (output truncated at 50000 chars)"
	}

	return result, nil
}

func execGlob(args map[string]interface{}) (string, error) {
	pattern := str(args, "pattern")
	basePath := str(args, "path")
	if pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	if basePath == "" {
		basePath = "."
	}

	var matches []string

	if strings.Contains(pattern, "**") {
		// Recursive walk for ** patterns
		parts := strings.SplitN(pattern, "**/", 2)
		var dir, filePattern string
		if len(parts) == 2 {
			dir = strings.TrimSuffix(parts[0], "/")
			if dir != "" {
				dir = filepath.Join(basePath, dir)
			} else {
				dir = basePath
			}
			filePattern = parts[1]
		} else {
			dir = basePath
			filePattern = "*"
		}

		filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				name := d.Name()
				if name == ".git" || name == "node_modules" || name == "vendor" ||
					name == "__pycache__" || name == ".next" || name == "target" ||
					name == "dist" || name == "build" {
					return filepath.SkipDir
				}
				return nil
			}
			if filePattern == "" || filePattern == "*" {
				matches = append(matches, path)
			} else {
				matched, _ := filepath.Match(filePattern, d.Name())
				if matched {
					matches = append(matches, path)
				}
			}
			if len(matches) >= 200 {
				return filepath.SkipAll
			}
			return nil
		})
	} else {
		// Simple glob
		fullPattern := filepath.Join(basePath, pattern)
		var err error
		matches, err = filepath.Glob(fullPattern)
		if err != nil {
			return "", fmt.Errorf("invalid pattern: %w", err)
		}
	}

	if len(matches) == 0 {
		return "No files found matching: " + pattern, nil
	}

	result := strings.Join(matches, "\n")
	if len(matches) >= 200 {
		result += "\n... (truncated at 200 results)"
	}
	return result, nil
}

func execGrep(args map[string]interface{}) (string, error) {
	pattern := str(args, "pattern")
	searchPath := str(args, "path")
	include := str(args, "include")

	if pattern == "" {
		return "", fmt.Errorf("pattern is required")
	}
	if searchPath == "" {
		searchPath = "."
	}

	// Parse context line counts
	before := 0
	after := 0
	if b := str(args, "before"); b != "" {
		fmt.Sscanf(b, "%d", &before)
	}
	if a := str(args, "after"); a != "" {
		fmt.Sscanf(a, "%d", &after)
	}
	if before > 10 {
		before = 10
	}
	if after > 10 {
		after = 10
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid regex %q: %w", pattern, err)
	}

	var results []string

	filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" ||
				name == "__pycache__" || name == ".next" || name == "target" ||
				name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply include filter
		if include != "" {
			matched, _ := filepath.Match(include, d.Name())
			if !matched {
				return nil
			}
		}

		// Skip binary files (heuristic: check first 512 bytes)
		info, err := d.Info()
		if err != nil || info.Size() > 10*1024*1024 {
			return nil // skip errors and files > 10MB
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(data), "\n")
		lastPrinted := -1
		for i, line := range lines {
			if re.MatchString(line) {
				if before > 0 || after > 0 {
					// Context mode: show surrounding lines
					start := i - before
					if start < 0 {
						start = 0
					}
					end := i + after
					if end >= len(lines) {
						end = len(lines) - 1
					}
					// Separator between non-contiguous groups
					if lastPrinted >= 0 && start > lastPrinted+1 {
						results = append(results, "--")
					}
					for j := start; j <= end; j++ {
						if j <= lastPrinted {
							continue // avoid duplicate lines
						}
						prefix := " "
						if j == i {
							prefix = ">"
						}
						results = append(results, fmt.Sprintf("%s%s:%d:%s", prefix, path, j+1, lines[j]))
						lastPrinted = j
					}
				} else {
					results = append(results, fmt.Sprintf("%s:%d:%s", path, i+1, line))
				}
				if len(results) >= 200 {
					return filepath.SkipAll
				}
			}
		}
		return nil
	})

	if len(results) == 0 {
		return "No matches found.", nil
	}

	result := strings.Join(results, "\n")
	if len(results) >= 200 {
		result += "\n... (truncated at 200 lines)"
	}
	return result, nil
}

func execRunTests(args map[string]interface{}) (string, error) {
	dir := str(args, "path")
	filter := str(args, "filter")
	if dir == "" {
		dir = "."
	}

	// Auto-detect test framework
	type testRunner struct {
		marker  string
		cmd     string
		args    []string
		filterF func(f string) []string
	}

	runners := []testRunner{
		{"go.mod", "go", []string{"test", "./..."}, func(f string) []string { return []string{"test", "./...", "-run", f} }},
		{"Cargo.toml", "cargo", []string{"test"}, func(f string) []string { return []string{"test", f} }},
		{"package.json", "npm", []string{"test"}, func(f string) []string { return []string{"test", "--", f} }},
		{"pyproject.toml", "pytest", nil, func(f string) []string { return []string{"-k", f} }},
		{"requirements.txt", "pytest", nil, func(f string) []string { return []string{"-k", f} }},
		{"Gemfile", "bundle", []string{"exec", "rspec"}, func(f string) []string { return []string{"exec", "rspec", "--pattern", f} }},
	}

	var found *testRunner
	for i, r := range runners {
		if _, err := os.Stat(filepath.Join(dir, r.marker)); err == nil {
			found = &runners[i]
			break
		}
	}

	if found == nil {
		return "", fmt.Errorf("could not detect test framework in %s (no go.mod, package.json, Cargo.toml, etc.)", dir)
	}

	cmdArgs := found.args
	if filter != "" && found.filterF != nil {
		cmdArgs = found.filterF(filter)
	}

	ctx, cancel := context.WithTimeout(context.Background(), envTimeout("GRUMP_TEST_TIMEOUT", 300))
	defer cancel()

	cmd := exec.CommandContext(ctx, found.cmd, cmdArgs...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()

	result := string(output)
	if len(result) > 50000 {
		result = result[:50000] + "\n... (output truncated at 50000 chars)"
	}

	if err != nil {
		if ctx.Err() != nil {
			return result + "\n(tests timed out after 5 minutes)", nil
		}
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return result + fmt.Sprintf("\nTests FAILED (exit code: %d)", exitCode), nil
	}

	return result + "\nTests PASSED", nil
}

func execListDirectory(args map[string]interface{}) (string, error) {
	path := str(args, "path")
	if path == "" {
		path = "."
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}

	var lines []string
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}

		name := e.Name()
		if e.IsDir() {
			name += "/"
		}

		sizeStr := formatSize(info.Size())
		modTime := info.ModTime().Format("Jan 02 15:04")

		lines = append(lines, fmt.Sprintf("%-40s %8s  %s", name, sizeStr, modTime))
	}

	if len(lines) == 0 {
		return "(empty directory)", nil
	}

	// Sort: directories first, then files
	sort.SliceStable(lines, func(i, j int) bool {
		iDir := strings.HasSuffix(strings.Fields(lines[i])[0], "/")
		jDir := strings.HasSuffix(strings.Fields(lines[j])[0], "/")
		if iDir != jDir {
			return iDir
		}
		return false
	})

	return strings.Join(lines, "\n"), nil
}

func formatSize(bytes int64) string {
	switch {
	case bytes >= 1024*1024*1024:
		return fmt.Sprintf("%.1fG", float64(bytes)/(1024*1024*1024))
	case bytes >= 1024*1024:
		return fmt.Sprintf("%.1fM", float64(bytes)/(1024*1024))
	case bytes >= 1024:
		return fmt.Sprintf("%.1fK", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func execBatchReplace(args map[string]interface{}, tracker *changes.Tracker) (string, error) {
	oldStr := str(args, "old_string")
	newStr := str(args, "new_string")
	include := str(args, "include")
	basePath := str(args, "path")

	if oldStr == "" {
		return "", fmt.Errorf("old_string is required")
	}
	if basePath == "" {
		basePath = "."
	}

	type fileResult struct {
		path  string
		count int
	}
	var results []fileResult
	totalCount := 0

	filepath.WalkDir(basePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" ||
				name == "__pycache__" || name == ".next" || name == "target" ||
				name == "dist" || name == "build" {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply include filter
		if include != "" {
			matched, _ := filepath.Match(include, d.Name())
			if !matched {
				return nil
			}
		}

		// Skip large files
		info, err := d.Info()
		if err != nil || info.Size() > 10*1024*1024 {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		content := string(data)
		count := strings.Count(content, oldStr)
		if count == 0 {
			return nil
		}

		// Record old content for undo
		if tracker != nil {
			tracker.Record("edit", path, content)
		}

		newContent := strings.ReplaceAll(content, oldStr, newStr)
		if err := os.WriteFile(path, []byte(newContent), info.Mode()); err != nil {
			return nil
		}

		results = append(results, fileResult{path: path, count: count})
		totalCount += count
		return nil
	})

	if len(results) == 0 {
		return fmt.Sprintf("No occurrences of %q found.", oldStr), nil
	}

	var summary []string
	for _, r := range results {
		summary = append(summary, fmt.Sprintf("%s (%d)", r.path, r.count))
	}
	return fmt.Sprintf("Replaced %d occurrences across %d files: %s", totalCount, len(results), strings.Join(summary, ", ")), nil
}

func execWatchFiles(args map[string]interface{}) (string, error) {
	pathsStr := str(args, "paths")
	durStr := str(args, "duration")
	if pathsStr == "" {
		return "", fmt.Errorf("paths is required")
	}

	duration := 10
	if durStr != "" {
		fmt.Sscanf(durStr, "%d", &duration)
	}
	if duration < 1 {
		duration = 1
	}
	if duration > 60 {
		duration = 60
	}

	// Resolve paths (support comma-separated, with optional glob expansion)
	rawPaths := strings.Split(pathsStr, ",")
	var filePaths []string
	for _, p := range rawPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		// Try glob expansion
		expanded, err := filepath.Glob(p)
		if err != nil || len(expanded) == 0 {
			filePaths = append(filePaths, p)
		} else {
			filePaths = append(filePaths, expanded...)
		}
	}

	if len(filePaths) == 0 {
		return "", fmt.Errorf("no valid paths provided")
	}

	// Record initial modification times
	type fileState struct {
		path    string
		oldTime time.Time
		newTime time.Time
		changed bool
	}
	states := make(map[string]*fileState)
	for _, p := range filePaths {
		info, err := os.Stat(p)
		if err != nil {
			states[p] = &fileState{path: p}
			continue
		}
		states[p] = &fileState{path: p, oldTime: info.ModTime()}
	}

	// Poll every 500ms
	deadline := time.Now().Add(time.Duration(duration) * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		<-ticker.C
		for _, st := range states {
			info, err := os.Stat(st.path)
			if err != nil {
				continue
			}
			if info.ModTime().After(st.oldTime) && !st.oldTime.IsZero() {
				st.changed = true
				st.newTime = info.ModTime()
			}
		}
	}

	// Build summary
	var changed []string
	for _, st := range states {
		if st.changed {
			changed = append(changed, fmt.Sprintf("  %s: %s -> %s",
				st.path,
				st.oldTime.Format("15:04:05.000"),
				st.newTime.Format("15:04:05.000")))
		}
	}

	if len(changed) == 0 {
		return fmt.Sprintf("No files changed during %d second watch period.", duration), nil
	}

	return fmt.Sprintf("Files changed during %ds watch:\n%s", duration, strings.Join(changed, "\n")), nil
}

// envTimeout reads an environment variable as seconds and returns a duration, or the default.
func envTimeout(envKey string, defaultSec int) time.Duration {
	if v := os.Getenv(envKey); v != "" {
		var sec int
		if _, err := fmt.Sscanf(v, "%d", &sec); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return time.Duration(defaultSec) * time.Second
}

// str safely extracts a string from a map.
func str(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprint(v)
	}
	return s
}
