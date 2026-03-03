package tools

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// safeBuf is a goroutine-safe bytes.Buffer for capturing process output.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

type Process struct {
	ID      int
	Command string
	Cmd     *exec.Cmd
	Output  *safeBuf
	exited  bool
	mu      sync.Mutex
}

func (p *Process) markExited() {
	p.mu.Lock()
	p.exited = true
	p.mu.Unlock()
}

func (p *Process) isExited() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exited
}

var (
	processes   = make(map[int]*Process)
	processLock sync.Mutex
	nextProcID  = 1
)

func ToolStartServer(args map[string]interface{}) (string, error) {
	command := str(args, "command")
	if command == "" {
		return "", fmt.Errorf("command is required")
	}

	cmd := exec.Command("bash", "-c", command)
	// Create a new process group so we can kill the whole tree (e.g. npm -> node)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	outBuf := &safeBuf{}
	cmd.Stdout = outBuf
	cmd.Stderr = outBuf

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start server: %v", err)
	}

	processLock.Lock()
	id := nextProcID
	nextProcID++
	proc := &Process{
		ID:      id,
		Command: command,
		Cmd:     cmd,
		Output:  outBuf,
	}
	processes[id] = proc
	processLock.Unlock()

	// Reap the process in a background goroutine so it doesn't become a zombie
	go func() {
		cmd.Wait()
		proc.markExited()
	}()

	// Wait briefly to see if it immediately crashes
	time.Sleep(1 * time.Second)

	if proc.isExited() {
		processLock.Lock()
		delete(processes, id)
		processLock.Unlock()
		return fmt.Sprintf("Server crashed immediately. Output:\n%s", outBuf.String()), nil
	}

	return fmt.Sprintf("Started background server successfully. PID: %d, ID: %d. Use list_servers to check status, or read_server_logs to see output.", cmd.Process.Pid, id), nil
}

func ToolListServers(args map[string]interface{}) (string, error) {
	processLock.Lock()
	defer processLock.Unlock()

	if len(processes) == 0 {
		return "No background servers are currently running.", nil
	}

	var sb bytes.Buffer
	sb.WriteString("Active Background Servers:\n")
	for id, proc := range processes {
		status := "Running"
		if proc.isExited() {
			status = "Exited"
		}
		sb.WriteString(fmt.Sprintf("[%d] PID: %d | Status: %s | Cmd: %s\n", id, proc.Cmd.Process.Pid, status, proc.Command))
	}
	return sb.String(), nil
}

func ToolReadServerLogs(args map[string]interface{}) (string, error) {
	idStr := str(args, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return "", fmt.Errorf("invalid ID")
	}

	processLock.Lock()
	proc, exists := processes[id]
	processLock.Unlock()

	if !exists {
		return "", fmt.Errorf("no server found with ID %d", id)
	}

	output := proc.Output.String()
	if len(output) > 5000 {
		output = "... [truncated] ...\n" + output[len(output)-5000:]
	}

	if output == "" {
		return "No output generated yet.", nil
	}
	return output, nil
}

func ToolStopServer(args map[string]interface{}) (string, error) {
	idStr := str(args, "id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return "", fmt.Errorf("invalid ID")
	}

	processLock.Lock()
	proc, exists := processes[id]
	if !exists {
		processLock.Unlock()
		return "", fmt.Errorf("no server found with ID %d", id)
	}
	delete(processes, id)
	processLock.Unlock()

	if proc.Cmd.Process != nil {
		// Kill the entire process group to ensure child processes (like node apps spawned by npm) die
		pgid, err := syscall.Getpgid(proc.Cmd.Process.Pid)
		if err == nil {
			syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			proc.Cmd.Process.Kill()
		}
	}

	return fmt.Sprintf("Stopped background server [%d]: %s", id, proc.Command), nil
}

// StopAll kills all running background processes. Called during graceful shutdown.
func StopAll() {
	processLock.Lock()
	defer processLock.Unlock()

	for id, proc := range processes {
		if proc.Cmd.Process != nil {
			pgid, err := syscall.Getpgid(proc.Cmd.Process.Pid)
			if err == nil {
				syscall.Kill(-pgid, syscall.SIGKILL)
			} else {
				proc.Cmd.Process.Kill()
			}
		}
		delete(processes, id)
	}
}
