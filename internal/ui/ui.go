package ui

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// ANSI — structural codes (never change)
const (
	Reset  = "\033[0m"
	Bold   = "\033[1m"
	Dim    = "\033[2m"

	HideCursor = "\033[?25l"
	ShowCursor = "\033[?25h"
	ClearLine  = "\033[2K"
	ClearDown  = "\033[J"
)

// ANSI — theme colors (swapped by SetTheme)
var (
	Purple = "\033[38;5;141m"
	Cyan   = "\033[38;5;117m"
	Red    = "\033[38;5;203m"
	Yellow = "\033[38;5;221m"
	Green  = "\033[38;5;114m"
	Gray   = "\033[38;5;245m"
	White  = "\033[38;5;255m"
)

// ─────────────────────── Banner ───────────────────────

func PrintBanner(version, model, cwd, projectInfo string, extras ...string) {
	r := Reset
	w := White + Bold
	g := Gray + Dim
	c := Cyan
	y := Yellow
	
	cRed := "\033[38;5;196m"
	cOrange := "\033[38;5;202m"
	cYellow := "\033[38;5;226m"

	fmt.Println()
	fmt.Printf("  %s      ██████████      %s\n", cRed, r)
	fmt.Printf("  %s    ██          ██    %s\n", cRed, r)
	fmt.Printf("  %s  ██  ████  ████  ██  %s\n", cOrange, r)
	fmt.Printf("  %s  ██              ██  %s\n", cOrange, r)
	fmt.Printf("  %s  ██    ██████    ██  %s\n", cYellow, r)
	fmt.Printf("  %s  ██  ██      ██  ██  %s\n", cYellow, r)
	fmt.Printf("  %s    ██          ██    %s\n", cRed, r)
	fmt.Printf("  %s      ██████████      %s\n", cRed, r)
	fmt.Printf("  %s━━━━━━━━━━━━━━━━━━━━━━%s\n", cOrange, r)
	fmt.Printf("  %s  %sG-Rump-CLI%s %s%s%s\n", cRed, w, r, g, version, r)
	fmt.Printf("  %s  MODEL: %s%s%s\n", g, c, strings.ToUpper(model), r)
	fmt.Printf("  %s  /help for documentation%s\n", g, r)
	fmt.Println()
	fmt.Printf("  %sPATH: %s%s\n", g, cwd, r)
	if projectInfo != "" {
		fmt.Printf("  %s%s%s%s\n", y, Bold, strings.ToUpper(projectInfo), r)
	}
	if len(extras) > 0 && extras[0] != "" {
		fmt.Printf("  %s%s%s\n", g, extras[0], r)
	}
	fmt.Println()
}

// ─────────────────────── Spinner ───────────────────────

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type Spinner struct {
	label   string
	mu      sync.Mutex
	done    chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func (s *Spinner) UpdateLabel(label string) {
	s.mu.Lock()
	s.label = label
	s.mu.Unlock()
}

func (s *Spinner) getLabel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.label
}

func StartSpinner(label string) *Spinner {
	s := &Spinner{
		label:   label,
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go s.run()
	return s
}

func (s *Spinner) run() {
	defer close(s.stopped)
	fmt.Print(HideCursor)
	defer fmt.Print(ShowCursor)

	i := 0
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	start := time.Now()

	for {
		select {
		case <-s.done:
			fmt.Printf("\r%s", ClearLine)
			return
		case <-ticker.C:
			frame := spinFrames[i%len(spinFrames)]
			elapsed := time.Since(start).Round(time.Second)
			fmt.Printf("\r    %s%s%s %s%s [%s]%s", Purple, frame, Reset, Dim, s.getLabel(), elapsed, Reset)
			i++
		}
	}
}

func (s *Spinner) Stop() {
	s.once.Do(func() {
		close(s.done)
		<-s.stopped
	})
}

// ─────────────────────── Permission Selector ───────────────────────

type PermissionResult int

const (
	PermissionAllow  PermissionResult = iota
	PermissionDeny
	PermissionAlways
)

func SelectPermission(toolName string) PermissionResult {
	options := []string{
		"Allow",
		"Deny",
		"Always allow " + toolName,
	}

	idx := SelectOption(options, 0)

	switch idx {
	case 0:
		fmt.Printf("    %s[+] Allowed%s\n", Green, Reset)
		return PermissionAllow
	case 2:
		fmt.Printf("    %s[+] Always allowed%s\n", Green, Reset)
		return PermissionAlways
	default:
		fmt.Printf("    %s[-] Denied%s\n", Red, Reset)
		return PermissionDeny
	}
}

func SelectOption(options []string, defaultIdx int) int {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return defaultIdx
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return defaultIdx
	}
	defer term.Restore(fd, oldState)

	selected := defaultIdx
	n := len(options)

	draw := func() {
		for i, opt := range options {
			fmt.Printf("\r%s", ClearLine)
			if i == selected {
				fmt.Printf("    %s%s> %s%s", Cyan, Bold, opt, Reset)
			} else {
				fmt.Printf("      %s%s%s", Dim, opt, Reset)
			}
			if i < n-1 {
				fmt.Print("\r\n")
			}
		}
	}

	fmt.Print(HideCursor)
	draw()

	buf := make([]byte, 3)
	for {
		nr, err := os.Stdin.Read(buf)
		if err != nil || nr == 0 {
			break
		}

		switch {
		case nr == 1 && (buf[0] == 13 || buf[0] == 10): // Enter
			goto done
		case nr == 1 && buf[0] == 3: // Ctrl+C
			selected = 1 // Deny
			goto done
		case nr >= 3 && buf[0] == 27 && buf[1] == 91 && buf[2] == 66: // Down
			selected = (selected + 1) % n
		case nr >= 3 && buf[0] == 27 && buf[1] == 91 && buf[2] == 65: // Up
			selected = (selected - 1 + n) % n
		default:
			continue
		}

		if n > 1 {
			fmt.Printf("\033[%dA", n-1)
		}
		fmt.Print("\r")
		draw()
	}

done:
	if n > 1 {
		fmt.Printf("\033[%dA", n-1)
	}
	fmt.Printf("\r%s%s", ClearDown, ShowCursor)
	return selected
}

// ─────────────────────── Tool Display ───────────────────────

func PrintToolCall(name string, args map[string]interface{}) {
	fmt.Println()
	label := strings.ToUpper(strings.ReplaceAll(name, "_", " "))
	
	val := getStr(args, "path")
	if val == "" { val = getStr(args, "url") }
	if val == "" { val = getStr(args, "command") }
	if val == "" { val = getStr(args, "pattern") }
	if val == "" { val = getStr(args, "query") }

	fmt.Printf("  %s%s[%s]%s %s\n", Purple, Bold, label, Reset, Gray+val+Reset)
	
	if name == "edit_file" {
		PrintDiff(getStr(args, "old_string"), getStr(args, "new_string"))
	}
}

func PrintDiff(old, newStr string) {
	for _, line := range strings.Split(old, "\n") {
		if strings.TrimSpace(line) == "" { continue }
		fmt.Printf("    %s- %s%s\n", Red, line, Reset)
	}
	for _, line := range strings.Split(newStr, "\n") {
		if strings.TrimSpace(line) == "" { continue }
		fmt.Printf("    %s+ %s%s\n", Green, line, Reset)
	}
}

func PrintToolResult(name string, result string) {
	switch name {
	case "read_file":
		lines := strings.Count(result, "\n")
		fmt.Printf("    %s(%d lines read)%s\n", Gray, lines, Reset)
	case "glob", "grep", "list_directory":
		count := strings.Count(strings.TrimSpace(result), "\n") + 1
		if strings.Contains(result, "No files") || strings.Contains(result, "No matches") {
			count = 0
		}
		fmt.Printf("    %s(%d results)%s\n", Gray, count, Reset)
	case "bash", "fetch_webpage", "search_web", "browser_action", "project_map", "delegate_task":
		fmt.Printf("    %s[COMPLETED]%s\n", Green, Reset)
	case "write_file", "edit_file", "save_memory":
		fmt.Printf("    %s[SUCCESS]%s\n", Green, Reset)
	default:
		fmt.Printf("    %s[SUCCESS]%s\n", Green, Reset)
	}
}

func PrintToolError(name string, err error) {
	fmt.Printf("    %s[-] %s: %v%s\n", Red, name, err, Reset)
}

func getStr(args map[string]interface{}, key string) string {
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
