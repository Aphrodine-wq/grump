package tools

import (
	"fmt"
	"os/exec"
	"strconv"
)

// execProjectMap uses the system 'tree' command if available, 
// otherwise falls back to a custom find/grep structure to build a project map.
func execProjectMap(args map[string]interface{}) (string, error) {
	path := str(args, "path")
	if path == "" {
		path = "."
	}

	depthStr := str(args, "depth")
	depth := 3
	if d, err := strconv.Atoi(depthStr); err == nil && d > 0 {
		depth = d
	}

	// Prefer tree if installed
	treeCmd := exec.Command("tree", "-L", strconv.Itoa(depth), "-a", "-I", ".git|node_modules|vendor|__pycache__|.next|dist|build|target", path)
	out, err := treeCmd.CombinedOutput()
	if err == nil {
		result := string(out)
		if len(result) > 20000 {
			result = result[:20000] + "\n... [truncated]"
		}
		return result, nil
	}

	// Fallback to find if tree is not available
	findCmd := exec.Command("bash", "-c", fmt.Sprintf(`find %s -maxdepth %d -not -path '*/\.git/*' -not -path '*/node_modules/*' -not -path '*/vendor/*' -not -path '*/dist/*' -not -path '*/build/*' | sort`, path, depth))
	out, err = findCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to generate map: %v\nOutput: %s", err, string(out))
	}

	result := string(out)
	if len(result) > 20000 {
		result = result[:20000] + "\n... [truncated]"
	}
	
	if result == "" {
		return "No files found or directory is empty.", nil
	}

	return "Project Map:\n" + result, nil
}