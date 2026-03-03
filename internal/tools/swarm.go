package tools

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// execDelegateTask implements the Swarm Sub-Agent delegation.
// It creates an isolated child process of g-rump-cli via stdin piping.
func execDelegateTask(args map[string]interface{}) (string, error) {
	task := str(args, "task")
	if task == "" {
		return "", fmt.Errorf("task is required")
	}

	// We pipe the task into g-rump-cli. The piped input handler will run the task and exit.
	// We give it a generous timeout for complex sub-tasks.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "g-rump-cli")
	cmd.Stdin = bytes.NewBufferString(task)

	var outBuf bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	
	output := outBuf.String()
	
	// We filter out the banner and keep the core output
	if err != nil {
		return fmt.Sprintf("Sub-agent encountered an error: %v\nOutput:\n%s\nStderr:\n%s", err, output, errBuf.String()), nil
	}

	// If output is massive, truncate it to the last 4000 characters which usually contain the final summary
	if len(output) > 10000 {
		output = "...[truncated]...\n" + output[len(output)-10000:]
	}

	return fmt.Sprintf("Sub-agent completed the task successfully.\n\n--- Sub-Agent Final Output ---\n%s", output), nil
}