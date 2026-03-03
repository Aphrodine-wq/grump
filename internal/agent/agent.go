package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"g-rump-cli/internal/changes"
	"g-rump-cli/internal/logging"
	"g-rump-cli/internal/ollama"
	"g-rump-cli/internal/project"
	"g-rump-cli/internal/tokens"
	"g-rump-cli/internal/tools"
	"g-rump-cli/internal/ui"
)

const defaultMaxTurns = 100

var toolSpinLabel = map[string]string{
	"read_file":        "Reading...",
	"write_file":       "Writing...",
	"edit_file":        "Editing...",
	"bash":             "Running...",
	"glob":             "Searching...",
	"grep":             "Searching...",
	"list_directory": "Listing...",
	"save_memory":    "Remembering...",
	"remember_project_fact": "Saving to .bu...",
	"start_server":     "Starting server...",
	"list_servers":     "Checking servers...",
	"read_server_logs": "Reading logs...",
	"stop_server":      "Stopping server...",
	"fetch_webpage":    "Fetching URL...",
	"search_web":       "Searching Web...",
	"browser_action":   "Automating browser...",
	"project_map":      "Mapping project...",
	"http_request":     "Requesting...",
	"watch_files":      "Watching files...",
	"run_tests":        "Running tests...",
	"batch_replace":    "Replacing...",
	"delegate_task":    "Delegating to Sub-Agent...",
}

const enhancedSystemPrompt = `You are G-Rump-CLI, the absolute STRONGEST AI coding assistant in the world, running locally in the user's terminal. You possess unparalleled, highly-complex architectural reasoning capabilities and are an elite, world-class expert in ALL forms of software engineering, especially **Website Creations**, high-performance backend systems, and modern web development (React, Next.js, HTML/CSS/JS, Tailwind, etc.). You write insanely optimized, production-grade code that impresses senior engineers.

## Available Tools
- **read_file**: Read file contents with line numbers. Always read before editing.
- **write_file**: Create or overwrite files. Creates parent dirs automatically.
- **edit_file**: Targeted find-and-replace. old_string must be unique in the file.
- **bash**: Execute shell commands (builds, tests, git, installs, etc.).
- **glob**: Find files by pattern. ** for recursive (e.g., **/*.go).
- **grep**: Search file contents with regex. Returns file:line:content.
- **list_directory**: List files/dirs with sizes and dates.
- **project_map**: Generate a deep tree visualization of the project directory.
- **remember_project_fact**: Add architectural, dependency, or rule knowledge specifically to this project's .bu/knowledge.json local database.
- **start_server**: Run a background process (like 'npm run dev'). Do not use 'bash' for dev servers, use this so you don't block the terminal.
- **fetch_webpage**: Fetch the HTML of ANY URL (e.g., http://localhost:3000, https://docs.python.org) and get a clean DOM snapshot. Use this to read documentation or verify your frontend components rendered correctly!
- **search_web**: Search the public internet using DuckDuckGo to find documentation, solutions, or learn about new frameworks.
- **read_server_logs**: Read the output of a background server.
- **browser_action**: Use a full headless Chromium browser via Playwright to natively interact with websites! (goto, click, fill, evaluate, extract_text). Use this to literally test UIs as if you were a human.
- **delegate_task**: SWARM CAPABILITY. Spawn an isolated, independent sub-agent to solve a complex sub-task for you in parallel. Provide it a massive, highly detailed instruction prompt.

## Guidelines
1. **Explore first**: Master the codebase context using project_map, glob, grep, and read_file before making changes. Automatically read the README.md or .cursorrules if you haven't yet.
2. **Read before edit**: You MUST read a file before editing it to ensure exact content matching.
3. **Surgical precision**: Use edit_file for targeted changes. Only use write_file for new files or complete rewrites.
4. **Exact matching**: The edit_file old_string must exactly match text in the file including whitespace and indentation.
5. **Architectural brilliance**: Explain your highly complex, sophisticated intent before executing. Show off your intelligence.
6. **Ask for clarification**: If a task is ambiguous, proactively ask the user.
7. **Empirical Verification**: Rigorously verify your changes by compiling, linting, or running tests.
8. **Resilience**: If a tool fails (e.g., edit_file fails due to a mismatched old_string), DO NOT GIVE UP. You MUST dynamically adapt, read the file again to find the exact string, and retry until successful.
9. **Concise but profound**: Keep responses focused, highly actionable, and insanely smart.
10. **Elite Quality**: Enforce rigorous typing, clean patterns, and extreme performance.
11. **Design Mandates**: NEVER use Emojis in any output or generated code. NEVER use CSS gradients or visual gradients in styling. ONLY use clean, flat, solid colors in all visual designs.
12. **Self-Optimization**: You have the power to switch your own operational mode if a task requires it. 
    - If a task is architecturally complex or requires a multi-step roadmap, you may output: <switch_mode>plan</switch_mode>.
    - If a task requires extreme, first-principles reasoning or deep debugging, you may output: <switch_mode>deep</switch_mode>.
    - To return to standard execution, output: <switch_mode>normal</switch_mode>.
    You should suggest a mode switch in your very first response if you detect high complexity.
13. **Recursive Evolution**: You are authorized to improve your own source code. If you identify a way to make yourself faster, more efficient, or more capable, you may suggest or use the '/improve' protocol to modify your own Go source code and redeploy.
14. **Swarm Intelligence**: If you are given a massive ticket (e.g. "Build a full stack app"), break it down and use 'delegate_task' to spawn sub-agents to build the backend while you build the frontend. You can orchestrate multiple agents.

## Error Recovery
- When a tool fails, read the error message carefully. It often contains a specific fix suggestion (e.g., "Increase with GRUMP_BASH_TIMEOUT").
- Do not retry the exact same failing command more than once without changing something (different args, reading the file first, checking permissions, etc.).
- If stuck after two attempts, explain the issue to the user and ask for guidance rather than looping.

## Context Management
- Be concise in responses. Avoid dumping entire file contents when a summary suffices.
- Prefer targeted tool calls: read specific line ranges, grep for specific patterns, glob for specific files.
- When an auto-compact warning appears, briefly summarize your recent progress before continuing so context is not lost.

## Process Management
- Before starting a server with start_server, check if the port is in use: bash "lsof -i :<port>".
- For long-running bash commands, prefer adding explicit timeouts (e.g., timeout 30 npm test).
- Always stop servers you started when they are no longer needed.`

type Agent struct {
	client       *ollama.Client
	model        string
	basePrompt   string
	planMode     bool
	acceptEdits  bool
	deepMode     bool
	activeSkill  string
	skillContent string
	messages     []ollama.Message
	toolDefsRaw  json.RawMessage // pre-serialized tool JSON
	autoAllow    map[string]bool
	tracker      *changes.Tracker
	project      *project.Context
	renderMode   bool
	contextLimit int // max tokens for context window (default 128000)
	onModeChange func(mode string)
	permMu       sync.Mutex // serializes permission prompts so they don't interleave
	displayMu    sync.Mutex // serializes tool output display so concurrent tools don't interleave

	onAutoAllow  func(toolName string)

	// Session metrics
	sessionStart  time.Time
	inputTokens   int
	outputTokens  int
	apiCallCount  int
	toolCallCount int

	// Token estimation cache
	cachedContextTokens int
	cachedMsgCount      int

	// Limits
	maxTurns int

	// Conversation checkpoints
	checkpoints map[string][]ollama.Message

	// File tracking
	touchedFiles map[string]string // path -> last action ("read", "write", "edit")
}

// New creates a new Agent. If proj is nil, project detection is skipped.
func New(client *ollama.Client, model, systemPrompt string, proj *project.Context) *Agent {
	mt := defaultMaxTurns
	if v := os.Getenv("GRUMP_MAX_TURNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			mt = n
		}
	}

	a := &Agent{
		client:       client,
		model:        model,
		toolDefsRaw:  ollama.PreSerializeTools(tools.Definitions()),
		autoAllow:    tools.AutoAllowed(),
		tracker:      changes.NewTracker(),
		messages:     make([]ollama.Message, 0, 64),
		contextLimit: 128000,
		sessionStart: time.Now(),
		touchedFiles: make(map[string]string),
		maxTurns:     mt,
		checkpoints:  make(map[string][]ollama.Message),
	}

	// Use provided project context or detect fresh
	if proj != nil {
		a.project = proj
	} else {
		p := project.Detect()
		a.project = p
	}

	// Build system prompt with project context
	prompt := systemPrompt
	if prompt == "" {
		prompt = enhancedSystemPrompt
	}

	// Wait for git info before building prompt
	a.project.WaitGit()
	a.basePrompt = prompt + "\n\n" + a.project.ForPrompt()

	a.updateSystemPrompt()

	return a
}

func (a *Agent) SetOnModeChange(fn func(string)) {
	a.onModeChange = fn
}

func (a *Agent) SetPlanMode(enabled bool) {
	a.planMode = enabled
	a.updateSystemPrompt()
}

func (a *Agent) SetAcceptEdits(enabled bool) {
	a.acceptEdits = enabled
}

func (a *Agent) SetDeepMode(enabled bool) {
	a.deepMode = enabled
	a.updateSystemPrompt()
}

func (a *Agent) SetRenderMode(enabled bool) {
	a.renderMode = enabled
}

func (a *Agent) RenderMode() bool {
	return a.renderMode
}

func (a *Agent) SetAutoAllow(toolName string, allowed bool) {
	a.autoAllow[toolName] = allowed
}

func (a *Agent) SetOnAutoAllow(fn func(string)) {
	a.onAutoAllow = fn
}

func (a *Agent) AutoAllowedTools() []string {
	defaults := tools.AutoAllowed()
	var custom []string
	for name, allowed := range a.autoAllow {
		if allowed && !defaults[name] {
			custom = append(custom, name)
		}
	}
	return custom
}

func (a *Agent) SetContextLimit(limit int) {
	if limit > 0 {
		a.contextLimit = limit
	}
}

func (a *Agent) ContextLimit() int {
	return a.contextLimit
}

func (a *Agent) SetSkill(name, content string) {
	a.activeSkill = name
	a.skillContent = content
	a.updateSystemPrompt()
}

func (a *Agent) ClearSkill() {
	a.activeSkill = ""
	a.skillContent = ""
	a.updateSystemPrompt()
}

func (a *Agent) updateSystemPrompt() {
	finalPrompt := a.basePrompt
	
	// Inject Memory
	memoryCtx := tools.GetGlobalMemoryContext()
	if memoryCtx != "" {
		finalPrompt += memoryCtx
	}
	
	projMemCtx := tools.GetProjectMemoryContext()
	if projMemCtx != "" {
		finalPrompt += projMemCtx
	}

	if a.planMode {
		finalPrompt += "\n\n**CRITICAL**: You are currently in PLAN MODE. You may use tools to explore the codebase (read_file, list_directory, grep, glob), but you MUST NOT use edit_file, write_file, or bash commands that modify state. Your goal is to formulate an incredibly structured, step-by-step architectural plan. You must format your final output exactly like this:\n\n### Plan\n1. [ ] Step 1 description\n2. [ ] Step 2 description\n\nAsk the user to approve this plan before continuing."
	}
	
	if a.deepMode {
		finalPrompt += `

=========================================
🔥 DEEP THINKING PROTOCOL INITIATED 🔥
=========================================
You are now in DEEP THINKING MODE. This is the most complex, rigorous, and exhaustive reasoning state. 
Before taking ANY action or responding, you MUST output a <thinking> block.
Inside your <thinking> block, you must:
1. Deconstruct the user's request into foundational first principles.
2. Consider edge cases, security implications, and performance bottlenecks.
3. Formulate a multi-branch decision tree (if X fails, then Y).
4. Evaluate architectural trade-offs.
5. Challenge your own assumptions (e.g., "Am I missing a file? Should I search first?").
Only after your internal monologue is complete may you emit tool calls or final responses. This ensures you execute with maximum effectiveness and absolute precision.`
	}

	if a.activeSkill != "" && a.skillContent != "" {
		finalPrompt += fmt.Sprintf("\n\n<activated_skill name=\"%s\">\n%s\n</activated_skill>\n\n**CRITICAL**: You MUST follow the expert procedural guidance found in the activated skill above for the duration of this task.", a.activeSkill, a.skillContent)
	}

	if len(a.messages) > 0 && a.messages[0].Role == "system" {
		a.messages[0].Content = finalPrompt
	} else {
		a.messages = append([]ollama.Message{{Role: "system", Content: finalPrompt}}, a.messages...)
	}
}

// Run processes a user message through the agentic tool-use loop.
func (a *Agent) Run(ctx context.Context, userInput string) error {
	a.messages = append(a.messages, ollama.Message{
		Role:    "user",
		Content: userInput,
	})

	for turn := 0; turn < a.maxTurns; turn++ {
		// Auto-compact if context exceeds 80% of token limit (with cached estimation)
		var ctxTokens int
		if len(a.messages) == a.cachedMsgCount && a.cachedContextTokens > 0 {
			ctxTokens = a.cachedContextTokens
		} else if len(a.messages) > a.cachedMsgCount && a.cachedMsgCount > 0 {
			// Only estimate new messages and add to cached total
			delta := 0
			for _, m := range a.messages[a.cachedMsgCount:] {
				delta += 4 + tokens.EstimateTokens(m.Content)
			}
			ctxTokens = a.cachedContextTokens + delta
		} else {
			ctxTokens = tokens.EstimateMessages(a.messages)
		}
		a.cachedContextTokens = ctxTokens
		a.cachedMsgCount = len(a.messages)

		threshold := a.contextLimit * 80 / 100
		if ctxTokens > threshold {
			targetTokens := a.contextLimit * 60 / 100
			removed, summary := a.CompactToTokens(targetTokens)
			if removed > 0 {
				// Reset cache after compaction
				a.cachedContextTokens = tokens.EstimateMessages(a.messages)
				a.cachedMsgCount = len(a.messages)
				fmt.Printf("\n  %s[!] Auto-pruned %d messages (~%s -> ~%s tokens, limit: %s)%s\n",
					ui.Dim, removed, tokens.FormatTokenCount(ctxTokens), tokens.FormatTokenCount(a.cachedContextTokens), tokens.FormatTokenCount(a.contextLimit), ui.Reset)
				if summary != "" {
					fmt.Printf("  %s  Pruned: %s%s\n", ui.Dim, summary, ui.Reset)
				}
			}
		}

		logging.Debugf("API call: model=%s messages=%d est_tokens=%d", a.model, len(a.messages), ctxTokens)

		req := ollama.ChatRequest{
			Model:    a.model,
			Messages: a.messages,
			Tools:    a.toolDefsRaw,
			Stream:   true,
		}

		// Spinner while waiting for first token
		spin := ui.StartSpinner("Thinking...")

		// Live streaming token counter using atomics
		var streamTokenCount int64
		streamStart := time.Now()

		// Goroutine to update spinner with live tok/s
		stopTicker := make(chan struct{})
		var stopTickerOnce sync.Once
		closeTicker := func() { stopTickerOnce.Do(func() { close(stopTicker) }) }
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-stopTicker:
					return
				case <-ticker.C:
					count := atomic.LoadInt64(&streamTokenCount)
					if count > 0 {
						elapsed := time.Since(streamStart).Seconds()
						if elapsed > 0 {
							tps := float64(count) / elapsed
							spin.UpdateLabel(fmt.Sprintf("Generating... %d tokens (%.1f tok/s)", count, tps))
						}
					}
				}
			}
		}()

		hasText := false
		var renderBuf strings.Builder
		result, err := a.client.ChatStream(ctx, req, func(token string) {
			atomic.AddInt64(&streamTokenCount, 1)
			if !hasText {
				closeTicker()
				spin.Stop()
				if !a.renderMode {
					fmt.Printf("\n%s%sG-Rump-CLI >%s ", ui.Bold, ui.Purple, ui.Reset)
				}
				hasText = true
			}
			if a.renderMode {
				renderBuf.WriteString(token)
			} else {
				fmt.Print(token)
			}
		})

		// Stop the live counter goroutine if still running
		closeTicker()
		spin.Stop() // idempotent — safe if already stopped

		if err != nil {
			return fmt.Errorf("%s", ollama.ClassifyError(err, a.client.Host(), a.model))
		}

		// Update session metrics
		a.apiCallCount++
		inTok := tokens.EstimateMessages(a.messages)
		outTok := tokens.EstimateTokens(result.Content)
		a.inputTokens += inTok
		a.outputTokens += outTok
		logging.Debugf("API response: in_tokens=%d out_tokens=%d tool_calls=%d", inTok, outTok, len(result.ToolCalls))
		for _, tc := range result.ToolCalls {
			a.outputTokens += tokens.EstimateTokens(string(tc.Function.Arguments))
		}

		// Mode switching detection
		if strings.Contains(result.Content, "<switch_mode>plan</switch_mode>") {
			a.SetPlanMode(true)
			a.SetDeepMode(false)
			if a.onModeChange != nil { a.onModeChange("plan") }
		} else if strings.Contains(result.Content, "<switch_mode>deep</switch_mode>") {
			a.SetPlanMode(false)
			a.SetDeepMode(true)
			if a.onModeChange != nil { a.onModeChange("deep") }
		} else if strings.Contains(result.Content, "<switch_mode>normal</switch_mode>") {
			a.SetPlanMode(false)
			a.SetDeepMode(false)
			if a.onModeChange != nil { a.onModeChange("normal") }
		}

		if hasText {
			if a.renderMode {
				// Render buffered markdown output
				fmt.Printf("\n%s%sG-Rump-CLI >%s\n", ui.Bold, ui.Purple, ui.Reset)
				fmt.Print(ui.RenderMarkdown(renderBuf.String()))
			} else {
				fmt.Println()
			}
			if result.Duration > 0 {
				tps := float64(0)
				if result.EvalDur > 0 {
					tps = float64(result.EvalCount) / (float64(result.EvalDur) / 1e9)
				}
				fmt.Printf("%s%s[%s | %.1f tok/s]%s\n",
					ui.Dim, ui.Gray, result.Duration.Round(time.Millisecond), tps, ui.Reset)
			}
		}

		// Build assistant message for history
		assistantMsg := ollama.Message{
			Role:    "assistant",
			Content: result.Content,
		}
		if len(result.ToolCalls) > 0 {
			assistantMsg.ToolCalls = result.ToolCalls
		}
		a.messages = append(a.messages, assistantMsg)

		// No tool calls → done
		if len(result.ToolCalls) == 0 {
			fmt.Println()
			return nil
		}

		// Execute tool calls (concurrently for speed, limited to 5 at a time)
		var wg sync.WaitGroup
		var mu sync.Mutex
		sem := make(chan struct{}, 5) // limit concurrent goroutines

		toolResults := make([]ollama.Message, len(result.ToolCalls))

		for i, tc := range result.ToolCalls {
			wg.Add(1)
			go func(idx int, call ollama.ToolCall) {
				defer wg.Done()
				sem <- struct{}{}        // acquire slot
				defer func() { <-sem }() // release slot

				toolRes := a.executeTool(call)

				mu.Lock()
				toolResults[idx] = ollama.Message{
					Role:    "tool",
					Content: toolRes,
				}
				mu.Unlock()
			}(i, tc)
		}

		wg.Wait()
		
		// Append all results to message history in order
		a.messages = append(a.messages, toolResults...)
	}

	fmt.Printf("\n%s%s[reached %d tool turns — stopping]%s\n", ui.Yellow, ui.Dim, a.maxTurns, ui.Reset)
	return nil
}

func (a *Agent) executeTool(tc ollama.ToolCall) string {
	name := tc.Function.Name

	args, err := tc.Function.ParseArgs()
	if err != nil {
		ui.PrintToolError(name, err)
		return fmt.Sprintf("Error parsing arguments: %v", err)
	}

	// Validate arguments before execution
	if valErr := tools.ValidateArgs(name, args); valErr != nil {
		ui.PrintToolError(name, valErr)
		return fmt.Sprintf("Validation error: %v", valErr)
	}

	// Display the tool call (serialized to prevent interleaving)
	a.displayMu.Lock()
	ui.PrintToolCall(name, args)
	a.displayMu.Unlock()

	// Permission check for non-auto-allowed tools or Accept Edits Mode.
	// Serialized with a mutex so concurrent tool calls don't interleave prompts.
	needsPermission := !a.autoAllow[name]
	if a.acceptEdits && (name == "write_file" || name == "edit_file" || name == "bash") {
		needsPermission = true
	}

	if needsPermission {
		a.permMu.Lock()
		perm := ui.SelectPermission(name)
		switch perm {
		case ui.PermissionAlways:
			if !a.acceptEdits || (name != "write_file" && name != "edit_file" && name != "bash") {
				a.autoAllow[name] = true
				if a.onAutoAllow != nil {
					a.onAutoAllow(name)
				}
			}
		case ui.PermissionDeny:
			a.permMu.Unlock()
			return "Permission denied by user. Do not retry unless the user asks."
		}
		a.permMu.Unlock()
	}

	// Retryable tools map — tools that are safe to auto-retry on transient failure
	retryable := map[string]bool{
		"read_file":             true,
		"glob":                  true,
		"grep":                  true,
		"list_directory":        true,
		"fetch_webpage":         true,
		"search_web":            true,
		"http_request":          true,
		"watch_files":           true,
		"list_servers":          true,
		"read_server_logs":      true,
		"project_map":           true,
		"run_tests":             true,
		"save_memory":           true,
		"remember_project_fact": true,
		"start_server":          true,
		"stop_server":           true,
		"browser_action":        true,
	}

	// Execute with spinner
	logging.Debugf("tool exec: %s args=%v", name, args)
	toolStart := time.Now()
	label := toolSpinLabel[name]
	if label == "" {
		label = "Working..."
	}
	spin := ui.StartSpinner(label)
	result, err := tools.Execute(name, args, a.tracker)
	spin.Stop()
	logging.Debugf("tool done: %s duration=%v result_len=%d err=%v", name, time.Since(toolStart), len(result), err)

	if err != nil {
		// Auto-retry once for retryable tools
		if retryable[name] {
			time.Sleep(1 * time.Second)
			spin2 := ui.StartSpinner(label + " (retry)")
			retryResult, retryErr := tools.Execute(name, args, a.tracker)
			spin2.Stop()
			if retryErr == nil {
				a.toolCallCount++
				a.displayMu.Lock()
				ui.PrintToolResult(name, retryResult)
				a.displayMu.Unlock()
				return "[Succeeded on retry] " + retryResult
			}
		}
		a.displayMu.Lock()
		ui.PrintToolError(name, err)
		a.displayMu.Unlock()
		return fmt.Sprintf("Error executing %s: %v", name, err)
	}

	a.toolCallCount++

	// Track touched files
	a.trackTouchedFile(name, args)

	a.displayMu.Lock()
	ui.PrintToolResult(name, result)
	a.displayMu.Unlock()
	return result
}

// trackTouchedFile records file paths from tool arguments.
func (a *Agent) trackTouchedFile(toolName string, args map[string]interface{}) {
	getPath := func() string {
		if v, ok := args["path"]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	switch toolName {
	case "read_file":
		if p := getPath(); p != "" {
			a.touchedFiles[p] = "read"
		}
	case "write_file":
		if p := getPath(); p != "" {
			a.touchedFiles[p] = "write"
		}
	case "edit_file":
		if p := getPath(); p != "" {
			a.touchedFiles[p] = "edit"
		}
	case "batch_replace":
		if p := getPath(); p != "" {
			a.touchedFiles[p] = "edit"
		}
	}
}

// SaveCheckpoint saves a deep copy of the current messages.
func (a *Agent) SaveCheckpoint(name string) {
	cp := make([]ollama.Message, len(a.messages))
	copy(cp, a.messages)
	a.checkpoints[name] = cp
}

// RestoreCheckpoint replaces messages with a saved checkpoint and resets the token cache.
func (a *Agent) RestoreCheckpoint(name string) bool {
	cp, ok := a.checkpoints[name]
	if !ok {
		return false
	}
	a.messages = make([]ollama.Message, len(cp))
	copy(a.messages, cp)
	a.cachedContextTokens = tokens.EstimateMessages(a.messages)
	a.cachedMsgCount = len(a.messages)
	return true
}

// Checkpoints returns a map of checkpoint names to message counts.
func (a *Agent) Checkpoints() map[string]int {
	result := make(map[string]int, len(a.checkpoints))
	for name, msgs := range a.checkpoints {
		result[name] = len(msgs)
	}
	return result
}

// MaxTurns returns the current max turns limit.
func (a *Agent) MaxTurns() int {
	return a.maxTurns
}

// SetMaxTurns sets the max turns limit.
func (a *Agent) SetMaxTurns(n int) {
	if n > 0 {
		a.maxTurns = n
	}
}

// TouchedFiles returns the map of file paths to last action.
func (a *Agent) TouchedFiles() map[string]string {
	return a.touchedFiles
}

// Tracker returns the change tracker for undo support.
func (a *Agent) Tracker() *changes.Tracker {
	return a.tracker
}

// Project returns the detected project context.
func (a *Agent) Project() *project.Context {
	return a.project
}

func (a *Agent) Clear() {
	var system []ollama.Message
	if len(a.messages) > 0 && a.messages[0].Role == "system" {
		system = append(system, a.messages[0])
	}
	a.messages = system
	a.tracker = changes.NewTracker()
}

func (a *Agent) SetClient(c *ollama.Client) {
	a.client = c
}

func (a *Agent) SetModel(model string) {
	a.model = model
}

func (a *Agent) SetSystemPrompt(prompt string) {
	if len(a.messages) > 0 && a.messages[0].Role == "system" {
		a.messages[0].Content = prompt
	} else {
		a.messages = append([]ollama.Message{{Role: "system", Content: prompt}}, a.messages...)
	}
}

func (a *Agent) MessageCount() int {
	count := 0
	for _, m := range a.messages {
		if m.Role != "system" {
			count++
		}
	}
	return count
}

func (a *Agent) Compact(keepLast int) int {
	var system []ollama.Message
	var rest []ollama.Message

	for _, m := range a.messages {
		if m.Role == "system" {
			system = append(system, m)
		} else {
			rest = append(rest, m)
		}
	}

	removed := 0
	if len(rest) > keepLast {
		removed = len(rest) - keepLast
		rest = rest[removed:]
	}

	a.messages = append(system, rest...)

	if removed > 0 {
		note := ollama.Message{
			Role:    "system",
			Content: fmt.Sprintf("[Context note: %d earlier messages were removed to save context.]", removed),
		}
		pos := len(system)
		a.messages = append(a.messages[:pos+1], a.messages[pos:]...)
		a.messages[pos] = note
	}

	return removed
}

// CompactDryRun returns the messages that would be removed by CompactToTokens
// without actually removing them.
func (a *Agent) CompactDryRun(targetTokens int) []ollama.Message {
	var system []ollama.Message
	var rest []ollama.Message

	for _, m := range a.messages {
		if m.Role == "system" {
			system = append(system, m)
		} else {
			rest = append(rest, m)
		}
	}

	minKeep := 10
	if minKeep > len(rest) {
		minKeep = len(rest)
	}

	var wouldRemove []ollama.Message
	tiers := []string{"tool", "assistant", "user"}

	for _, tier := range tiers {
		for {
			current := tokens.EstimateMessages(append(system, rest...))
			if current <= targetTokens || len(rest) <= minKeep {
				break
			}
			removable := len(rest) - minKeep
			found := false
			for i := 0; i < removable; i++ {
				if rest[i].Role == tier {
					wouldRemove = append(wouldRemove, rest[i])
					rest = append(rest[:i], rest[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				break
			}
		}
		current := tokens.EstimateMessages(append(system, rest...))
		if current <= targetTokens {
			break
		}
	}

	return wouldRemove
}

// SessionStats holds aggregated metrics for the current session.
type SessionStats struct {
	Model         string
	Duration      time.Duration
	APICallCount  int
	ToolCallCount int
	InputTokens   int
	OutputTokens  int
	TotalTokens   int
	ContextTokens int
	ContextLimit  int
}

// Stats returns aggregated session metrics.
func (a *Agent) Stats() SessionStats {
	ctxTokens := tokens.EstimateMessages(a.messages)
	return SessionStats{
		Model:         a.model,
		Duration:      time.Since(a.sessionStart),
		APICallCount:  a.apiCallCount,
		ToolCallCount: a.toolCallCount,
		InputTokens:   a.inputTokens,
		OutputTokens:  a.outputTokens,
		TotalTokens:   a.inputTokens + a.outputTokens,
		ContextTokens: ctxTokens,
		ContextLimit:  a.contextLimit,
	}
}

// CompactToTokens removes oldest non-system messages until estimated tokens
// are at or below the target, keeping at least the last 10 messages.
// Uses tiered strategy: removes tool results first, then assistant, then user.
// Returns the number of removed messages and a summary of what was pruned.
func (a *Agent) CompactToTokens(targetTokens int) (int, string) {
	var system []ollama.Message
	var rest []ollama.Message

	for _, m := range a.messages {
		if m.Role == "system" {
			system = append(system, m)
		} else {
			rest = append(rest, m)
		}
	}

	minKeep := 10
	if minKeep > len(rest) {
		minKeep = len(rest)
	}

	// Tiered removal: tool -> assistant -> user (from oldest, outside minKeep)
	var pruned []ollama.Message
	typeCounts := map[string]int{"tool": 0, "assistant": 0, "user": 0}
	tiers := []string{"tool", "assistant", "user"}

	for _, tier := range tiers {
		for {
			current := tokens.EstimateMessages(append(system, rest...))
			if current <= targetTokens || len(rest) <= minKeep {
				break
			}
			// Find oldest message of this tier in the removable range
			removable := len(rest) - minKeep
			found := false
			for i := 0; i < removable; i++ {
				if rest[i].Role == tier {
					pruned = append(pruned, rest[i])
					typeCounts[tier]++
					rest = append(rest[:i], rest[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				break
			}
		}
		current := tokens.EstimateMessages(append(system, rest...))
		if current <= targetTokens {
			break
		}
	}

	removed := len(pruned)
	a.messages = append(system, rest...)

	if removed > 0 {
		note := ollama.Message{
			Role:    "system",
			Content: fmt.Sprintf("[Context note: %d earlier messages were removed to stay within token budget.]", removed),
		}
		pos := len(system)
		a.messages = append(a.messages[:pos+1], a.messages[pos:]...)
		a.messages[pos] = note
	}

	// Build summary with type breakdown
	var summary string
	if removed > 0 {
		var typeParts []string
		for _, tier := range tiers {
			if typeCounts[tier] > 0 {
				typeParts = append(typeParts, fmt.Sprintf("%d %s", typeCounts[tier], tier))
			}
		}
		summary = strings.Join(typeParts, ", ")

		// Add message previews (up to 5)
		var previews []string
		limit := removed
		if limit > 5 {
			limit = 5
		}
		for i := 0; i < limit; i++ {
			content := pruned[i].Content
			if len(content) > 40 {
				content = content[:40] + "..."
			}
			content = strings.ReplaceAll(content, "\n", " ")
			previews = append(previews, fmt.Sprintf("[%s] \"%s\"", pruned[i].Role, content))
		}
		summary += " — " + strings.Join(previews, " | ")
		if removed > 5 {
			summary += fmt.Sprintf(" | ... and %d more", removed-5)
		}
	}

	return removed, summary
}

// Messages returns the full message history (for session saving).
func (a *Agent) Messages() []ollama.Message {
	return a.messages
}

// LoadMessages restores conversation history (for session resuming).
func (a *Agent) LoadMessages(msgs []ollama.Message) {
	a.messages = msgs
}

func (a *Agent) MarshalHistory() ([]byte, error) {
	return json.MarshalIndent(a.messages, "", "  ")
}
