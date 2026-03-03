//go:build windows

package tools

import (
	"os/exec"
)

// setProcGroup is a no-op on Windows (process groups work differently).
func setProcGroup(cmd *exec.Cmd) {
	// Windows uses job objects; for now just skip process group setup.
}

// killProcessGroup kills the process on Windows (no UNIX-style process groups).
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process != nil {
		return cmd.Process.Kill()
	}
	return nil
}

// cancelProcess returns a cancel function that kills the process.
func cancelProcess(cmd *exec.Cmd) func() error {
	return func() error {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
