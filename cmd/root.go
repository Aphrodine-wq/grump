package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/chzyer/readline"
	"github.com/spf13/cobra"

	"g-rump-cli/internal/agent"
	"g-rump-cli/internal/logging"
	"g-rump-cli/internal/ollama"
	"g-rump-cli/internal/project"
	"g-rump-cli/internal/session"
	"g-rump-cli/internal/skills"
	"g-rump-cli/internal/tokens"
	"g-rump-cli/internal/tools"
	"g-rump-cli/internal/ui"
)

var (
	version = "2.0.0"
	commit  = "dev"
)

const defaultSystemPrompt = "" // empty means agent uses its enhanced built-in prompt

type Config struct {
	Model        string `json:"model"`
	Host         string `json:"host"`
	APIKey       string `json:"api_key,omitempty"`
	SystemPrompt string `json:"system_prompt"`
	Theme        string `json:"theme,omitempty"`
	PlanMode     bool   `json:"plan_mode,omitempty"`
	AcceptEdits  bool   `json:"accept_edits,omitempty"`
	DeepMode     bool   `json:"deep_mode,omitempty"`
	RenderMode   bool     `json:"render_mode,omitempty"`
	AutoAllow    []string          `json:"auto_allow,omitempty"`
	Aliases      map[string]string `json:"aliases,omitempty"`
}

var cfg Config

var rootCmd = &cobra.Command{
	Use:     "g-rump-cli",
	Short:   "AI coding assistant powered by Ollama",
	Long:    "G-Rump-CLI is an agentic AI coding assistant that can read, write, edit files and run commands.",
	Version: version,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runREPL()
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%s%sError: %v%s\n", ui.Bold, ui.Red, err, ui.Reset)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&cfg.Model, "model", "m", "",
		"model name (default: glm-5:cloud)")
	rootCmd.PersistentFlags().StringVar(&cfg.Host, "host", "",
		"Ollama host URL (default: http://localhost:11434)")
	rootCmd.PersistentFlags().StringVarP(&cfg.SystemPrompt, "system", "s", "",
		"system prompt override")
	rootCmd.PersistentFlags().BoolVar(&logging.Debug, "debug", false,
		"enable debug logging to stderr")

	cobra.OnInitialize(loadConfig)
}

// isFirstRun returns true when no config file exists yet.
func isFirstRun() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, ".g-rump-cli", "config.json"))
	return os.IsNotExist(err)
}

func loadConfig() {
	defaults := Config{
		Model:        "glm-5:cloud",
		Host:         "http://localhost:11434",
		SystemPrompt: defaultSystemPrompt,
		Theme:        "dark",
	}

	home, err := os.UserHomeDir()
	if err == nil {
		data, err := os.ReadFile(filepath.Join(home, ".g-rump-cli", "config.json"))
		if err == nil {
			json.Unmarshal(data, &defaults)
		}
	}

	if cfg.Model == "" {
		cfg.Model = defaults.Model
	}
	if cfg.Host == "" {
		cfg.Host = defaults.Host
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = defaults.SystemPrompt
	}
	if cfg.Theme == "" {
		cfg.Theme = defaults.Theme
	}

	// Apply theme
	if cfg.Theme != "" {
		ui.SetTheme(cfg.Theme)
	}
}

func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".g-rump-cli")
}

func saveConfig(c Config) error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0644)
}

func runREPL() error {
	// First-run walkthrough
	if isFirstRun() {
		cfg = runWalkthrough(cfg.Host)
		if err := saveConfig(cfg); err != nil {
			fmt.Printf("  %s[-] Could not save config: %v%s\n", ui.Red, err, ui.Reset)
		}
	}

	cwd, _ := os.Getwd()

	// Fast project detection (no subprocess) — git info fills in background
	proj := project.DetectFast()
	go proj.FillGitInfo()

	// Pre-warm Ollama connection during banner display
	client := ollama.NewClient(cfg.Host, cfg.APIKey)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		client.Ping(ctx)
	}()

	// Build extras for enhanced banner
	var extraParts []string

	// Git status summary
	if gitStatus, err := exec.Command("git", "status", "--porcelain").Output(); err == nil {
		lines := strings.Split(strings.TrimSpace(string(gitStatus)), "\n")
		modified, untracked := 0, 0
		for _, line := range lines {
			if len(line) < 2 {
				continue
			}
			if line[0] == '?' {
				untracked++
			} else {
				modified++
			}
		}
		if modified > 0 || untracked > 0 {
			extraParts = append(extraParts, fmt.Sprintf("Git: %d modified, %d untracked", modified, untracked))
		}
	}

	// Last session info
	if sessions := session.List(); len(sessions) > 0 {
		last := sessions[0]
		ago := time.Since(last.Updated).Round(time.Minute)
		extraParts = append(extraParts, fmt.Sprintf("Last session: %s (%s ago)", last.Name, ago))
	}

	// Random tip
	tips := []string{
		"Tip: Use /render for rich markdown output",
		"Tip: Use /diff to analyze uncommitted changes",
		"Tip: Use /search to find things in your conversation",
		"Tip: Use /commit for AI-generated commit messages",
		"Tip: Use /blame <file> for git blame analysis",
		"Tip: Use /env to see your development environment",
		"Tip: Use /mode to cycle between Normal, Plan, and Deep modes",
		"Tip: Use /alias name=/command to create shortcuts",
		"Tip: Use /stash to quickly save and restore changes",
		"Tip: Use /compact to free up context when running low",
	}
	extraParts = append(extraParts, tips[time.Now().UnixNano()%int64(len(tips))])

	extras := strings.Join(extraParts, "  |  ")
	ui.PrintBanner(version, cfg.Model, cwd, proj.StatusLine(), extras)

	getPrompt := func() string {
		modeStr := ""
		if cfg.PlanMode {
			modeStr = fmt.Sprintf("%s[PLAN]%s ", ui.Cyan+ui.Bold, ui.Reset)
		} else if cfg.DeepMode {
			modeStr = fmt.Sprintf("%s[DEEP]%s ", ui.Purple+ui.Bold, ui.Reset)
		}
		return fmt.Sprintf("%s%s%sYou >%s ", modeStr, ui.Bold, ui.Cyan, ui.Reset)
	}

	readyMsg := fmt.Sprintf("%s%sReady. Describe what you need, or type /help.%s\n\n", ui.Dim, ui.Gray, ui.Reset)

	ag := agent.New(client, cfg.Model, cfg.SystemPrompt, proj)
	ag.SetPlanMode(cfg.PlanMode)
	ag.SetDeepMode(cfg.DeepMode)
	ag.SetAcceptEdits(cfg.AcceptEdits)
	ag.SetRenderMode(cfg.RenderMode)

	// Restore persisted tool permissions
	for _, name := range cfg.AutoAllow {
		ag.SetAutoAllow(name, true)
	}

	// Persist new "Always allow" choices to config
	ag.SetOnAutoAllow(func(toolName string) {
		// Add if not already in list
		for _, existing := range cfg.AutoAllow {
			if existing == toolName {
				return
			}
		}
		cfg.AutoAllow = append(cfg.AutoAllow, toolName)
		saveConfig(cfg)
	})

	ag.SetOnModeChange(func(mode string) {
		switch mode {
		case "plan":
			cfg.PlanMode = true
			cfg.DeepMode = false
			fmt.Printf("\n  %s[!] Auto-switched to PLAN mode for architectural complexity.%s\n", ui.Cyan+ui.Bold, ui.Reset)
		case "deep":
			cfg.PlanMode = false
			cfg.DeepMode = true
			fmt.Printf("\n  %s[!] Auto-switched to DEEP mode for rigorous analysis.%s\n", ui.Purple+ui.Bold, ui.Reset)
		case "normal":
			cfg.PlanMode = false
			cfg.DeepMode = false
			fmt.Printf("\n  %s[!] Switched to NORMAL mode.%s\n", ui.Green, ui.Reset)
		}
		// Force prompt update in readline if we could, but setting it for next loop is simpler
	})

	// Check for piped input
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		return handlePipedInput(ag)
	}

	completer := readline.NewPrefixCompleter(
		readline.PcItem("/clear"),
		readline.PcItem("/context"),
		readline.PcItem("/compact"),
		readline.PcItem("/model"),
		readline.PcItem("/host"),
		readline.PcItem("/key"),
		readline.PcItem("/system"),
		readline.PcItem("/mode"),
		readline.PcItem("/plan"),
		readline.PcItem("/deep"),
		readline.PcItem("/accept"),
		readline.PcItem("/render"),
		readline.PcItem("/context-limit"),
		readline.PcItem("/max-turns"),
		readline.PcItem("/permissions"),
		readline.PcItem("/theme",
			readline.PcItem("dark"),
			readline.PcItem("light"),
		),
		readline.PcItem("/skills"),
		readline.PcItem("/skill"),
		readline.PcItem("/gsd"),
		readline.PcItem("/ralph"),
		readline.PcItem("/clearskill"),
		readline.PcItem("/stats"),
		readline.PcItem("/history"),
		readline.PcItem("/cd"),
		readline.PcItem("/log"),
		readline.PcItem("/diff"),
		readline.PcItem("/undo"),
		readline.PcItem("/changes"),
		readline.PcItem("/session"),
		readline.PcItem("/paste"),
		readline.PcItem("/export"),
		readline.PcItem("/sessions"),
		readline.PcItem("/resume"),
		readline.PcItem("/improve"),
		readline.PcItem("/install"),
		readline.PcItem("/search"),
		readline.PcItem("/drop"),
		readline.PcItem("/tokens"),
		readline.PcItem("/retry"),
		readline.PcItem("/commit"),
		readline.PcItem("/save"),
		readline.PcItem("/blame"),
		readline.PcItem("/env"),
		readline.PcItem("/alias"),
		readline.PcItem("/stash"),
		readline.PcItem("/files"),
		readline.PcItem("/checkpoint"),
		readline.PcItem("/restore"),
		readline.PcItem("/help"),
		readline.PcItem("/exit"),
		readline.PcItem("/quit"),
		readline.PcItem("/init"),
	)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          getPrompt(),
		HistoryFile:     filepath.Join(configDir(), "history"),
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		fmt.Printf("Error starting readline: %v\n", err)
		return err
	}
	defer rl.Close()

	// Special key listener for mode cycling (Tab)
	// We'll use a custom logic: if input is empty and Tab is pressed, cycle mode.
	// But readline already uses Tab for completion. 
	// Let's use a custom command or just rely on /mode which is fast.
	// To truly meet the "Shift+Tab" requirement without breaking readline, we'd need a very complex raw listener.
	// Let's stick to the /mode command for now as it's more stable in this environment.

	fmt.Print(readyMsg)

	// Session tracking
	var currentSessionID string

	for {
		rl.SetPrompt(getPrompt())
		line, err := rl.Readline()
		if err != nil { // EOF or Ctrl-D
			break
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		// Multi-line input: start with """ and end with """
		if input == `"""` {
			input = readMultiLineRL(rl)
			if input == "" {
				continue
			}
		} else if strings.HasSuffix(input, `\`) {
			// Line continuation with backslash
			var lines []string
			lines = append(lines, strings.TrimSuffix(input, `\`))
			rl.SetPrompt(fmt.Sprintf("%s%s  . >%s ", ui.Dim, ui.Cyan, ui.Reset))
			for {
				contLine, err := rl.Readline()
				if err != nil {
					break
				}
				if strings.HasSuffix(strings.TrimSpace(contLine), `\`) {
					lines = append(lines, strings.TrimSuffix(strings.TrimSpace(contLine), `\`))
				} else {
					lines = append(lines, strings.TrimSpace(contLine))
					break
				}
			}
			rl.SetPrompt(getPrompt())
			input = strings.Join(lines, "\n")
		}

		if strings.HasPrefix(input, "/") {
			if handleCommand(input, ag, &currentSessionID) {
				break
			}
			continue
		}

		// Trap Ctrl+C during agent execution
		runCtx, runCancel := context.WithCancel(context.Background())
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt)

		go func() {
			select {
			case <-sigCh:
				runCancel()
			case <-runCtx.Done():
			}
		}()

		err = ag.Run(runCtx, input)

		signal.Stop(sigCh)
		signal.Reset(os.Interrupt)
		runCancel()

		if err != nil {
			// Check if the error itself is a context cancellation from the user pressing Ctrl+C
			if err == context.Canceled || strings.Contains(err.Error(), "context canceled") {
				fmt.Printf("\n%s%s[interrupted]%s\n\n", ui.Dim, ui.Yellow, ui.Reset)
				continue
			}
			fmt.Printf("\n%s%sError: %v%s\n\n", ui.Bold, ui.Red, err, ui.Reset)
		}

		// Auto-save session if we have one
		if currentSessionID != "" {
			autoSaveSession(ag, currentSessionID)
		}
	}

	// Kill any remaining background server processes
	tools.StopAll()

	// Print changes summary on exit
	tracker := ag.Tracker()
	if tracker.Count() > 0 {
		fmt.Printf("\n%s%s%s%s\n", ui.Dim, ui.Gray, tracker.Summary(), ui.Reset)
	}

	fmt.Printf("%s%sGoodbye!%s\n", ui.Dim, ui.Purple, ui.Reset)
	return nil
}

func readMultiLineRL(rl *readline.Instance) string {
	fmt.Printf("%s%s(multi-line mode — type \"\"\" to end)%s\n", ui.Dim, ui.Gray, ui.Reset)
	var lines []string
	oldPrompt := rl.Config.Prompt
	rl.SetPrompt(fmt.Sprintf("%s%s  . >%s ", ui.Dim, ui.Cyan, ui.Reset))
	defer rl.SetPrompt(oldPrompt)
	
	for {
		line, err := rl.Readline()
		if err != nil {
			break
		}
		if strings.TrimSpace(line) == `"""` {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func readMultiLine(scanner *bufio.Scanner) string {
	fmt.Printf("%s%s(multi-line mode — type \"\"\" to end)%s\n", ui.Dim, ui.Gray, ui.Reset)
	var lines []string
	for {
		fmt.Printf("%s%s  . >%s ", ui.Dim, ui.Cyan, ui.Reset)
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == `"""` {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func handlePipedInput(ag *agent.Agent) error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading piped input: %w", err)
	}

	input := strings.TrimSpace(string(data))
	if input == "" {
		return nil
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	return ag.Run(ctx, input)
}

func autoSaveSession(ag *agent.Agent, sessionID string) {
	sess, err := session.Load(sessionID)
	if err != nil {
		return
	}
	sess.Messages = ag.Messages()
	session.Save(*sess)
}

func handleCommand(input string, ag *agent.Agent, sessionID *string) bool {
	parts := strings.Fields(input)
	command := strings.ToLower(parts[0])

	// Alias expansion: check if command matches an alias key
	if cfg.Aliases != nil {
		aliasKey := strings.TrimPrefix(command, "/")
		if expansion, ok := cfg.Aliases[aliasKey]; ok {
			// Replace command with expansion, keep any extra args
			expanded := expansion
			if len(parts) > 1 {
				expanded += " " + strings.Join(parts[1:], " ")
			}
			parts = strings.Fields(expanded)
			command = strings.ToLower(parts[0])
		}
	}

	switch command {
	case "/exit", "/quit", "/q":
		return true

	case "/mode":
		if cfg.PlanMode {
			// Currently Plan -> switch to Deep
			cfg.PlanMode = false
			cfg.DeepMode = true
			ag.SetPlanMode(false)
			ag.SetDeepMode(true)
			fmt.Printf("  %s[+] Mode: DEEP THINKING (Maximum complexity analysis).%s\n\n", ui.Purple+ui.Bold, ui.Reset)
		} else if cfg.DeepMode {
			// Currently Deep -> switch to Normal
			cfg.DeepMode = false
			ag.SetDeepMode(false)
			fmt.Printf("  %s[+] Mode: NORMAL (Standard coding).%s\n\n", ui.Green, ui.Reset)
		} else {
			// Currently Normal -> switch to Plan
			cfg.PlanMode = true
			ag.SetPlanMode(true)
			fmt.Printf("  %s[+] Mode: PLAN (Read-only architectural planning).%s\n\n", ui.Cyan+ui.Bold, ui.Reset)
		}

	case "/init":
		cwd, _ := os.Getwd()
		buDir := filepath.Join(cwd, ".bu")
		if err := os.MkdirAll(buDir, 0755); err != nil {
			fmt.Printf("  %s[-] Failed to initialize .bu directory: %v%s\n\n", ui.Red, err, ui.Reset)
		} else {
			readmePath := filepath.Join(buDir, "README.md")
			os.WriteFile(readmePath, []byte("# G-Rump-CLI Project Memory\n\nThis directory contains the local knowledge graph and custom rules for this project.\n- `knowledge.json` is updated by the agent using `remember_project_fact`.\n"), 0644)
			fmt.Printf("  %s[+] Initialized .bu local project memory in %s%s\n\n", ui.Green, cwd, ui.Reset)
		}

	case "/plan":
		cfg.PlanMode = !cfg.PlanMode
		ag.SetPlanMode(cfg.PlanMode)
		if cfg.PlanMode {
			fmt.Printf("  %s[+] Plan Mode enabled (Agent will only read files and create plans).%s\n\n", ui.Green, ui.Reset)
		} else {
			fmt.Printf("  %s[+] Plan Mode disabled.%s\n\n", ui.Green, ui.Reset)
		}

	case "/deep":
		cfg.DeepMode = !cfg.DeepMode
		ag.SetDeepMode(cfg.DeepMode)
		if cfg.DeepMode {
			fmt.Printf("  %s[+] Deep Thinking Protocol ENABLED. Agent will now analyze requests recursively before acting.%s\n\n", ui.Purple+ui.Bold, ui.Reset)
		} else {
			fmt.Printf("  %s[+] Deep Thinking Protocol disabled.%s\n\n", ui.Green, ui.Reset)
		}

	case "/accept":
		cfg.AcceptEdits = !cfg.AcceptEdits
		ag.SetAcceptEdits(cfg.AcceptEdits)
		if cfg.AcceptEdits {
			fmt.Printf("  %s[+] Accept Edits Mode enabled (Agent will prompt before writing/editing files).%s\n\n", ui.Green, ui.Reset)
		} else {
			fmt.Printf("  %s[+] Accept Edits Mode disabled.%s\n\n", ui.Green, ui.Reset)
		}

	case "/render":
		cfg.RenderMode = !cfg.RenderMode
		ag.SetRenderMode(cfg.RenderMode)
		if cfg.RenderMode {
			fmt.Printf("  %s[+] Render Mode enabled (Rich markdown output with syntax highlighting).%s\n\n", ui.Green, ui.Reset)
		} else {
			fmt.Printf("  %s[+] Render Mode disabled (Raw streaming output).%s\n\n", ui.Green, ui.Reset)
		}

	case "/context-limit":
		if len(parts) < 2 {
			fmt.Printf("  %sCurrent context limit: %d tokens%s\n\n", ui.Gray, ag.ContextLimit(), ui.Reset)
		} else {
			var limit int
			if _, err := fmt.Sscanf(parts[1], "%d", &limit); err != nil || limit < 1000 {
				fmt.Printf("  %s[-] Invalid limit. Provide a number >= 1000 (e.g., /context-limit 32000)%s\n\n", ui.Red, ui.Reset)
			} else {
				ag.SetContextLimit(limit)
				fmt.Printf("  %s[+] Context limit set to %d tokens%s\n\n", ui.Green, limit, ui.Reset)
			}
		}

	case "/max-turns":
		if len(parts) < 2 {
			fmt.Printf("  %sCurrent max turns: %d%s\n\n", ui.Gray, ag.MaxTurns(), ui.Reset)
		} else {
			var n int
			if _, err := fmt.Sscanf(parts[1], "%d", &n); err != nil || n < 1 {
				fmt.Printf("  %s[-] Invalid value. Provide a positive integer (e.g., /max-turns 50)%s\n\n", ui.Red, ui.Reset)
			} else {
				ag.SetMaxTurns(n)
				fmt.Printf("  %s[+] Max turns set to %d%s\n\n", ui.Green, n, ui.Reset)
			}
		}

	case "/permissions":
		defaults := tools.AutoAllowed()
		fmt.Printf("\n%s%s  Tool Permissions%s\n", ui.Bold, ui.White, ui.Reset)
		fmt.Printf("  %s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", ui.Gray, ui.Reset)
		allTools := tools.Definitions()
		for _, t := range allTools {
			name := t.Function.Name
			status := fmt.Sprintf("%s[ask]%s", ui.Yellow, ui.Reset)
			if defaults[name] {
				status = fmt.Sprintf("%s[auto]%s", ui.Green, ui.Reset)
			}
			// Check if user granted "always allow"
			for _, allowed := range cfg.AutoAllow {
				if allowed == name {
					status = fmt.Sprintf("%s[always]%s", ui.Cyan, ui.Reset)
					break
				}
			}
			fmt.Printf("  %-20s %s\n", name, status)
		}
		fmt.Printf("\n  %s[auto] = read-only default  [always] = user-granted  [ask] = prompts each time%s\n\n", ui.Gray, ui.Reset)

	case "/clear":
		ag.Clear()
		fmt.Printf("  %s[+] Conversation cleared.%s\n\n", ui.Green, ui.Reset)

	case "/model":
		if len(parts) < 2 {
			// Interactive model selector
			client := ollama.NewClient(cfg.Host, cfg.APIKey)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			localModels, listErr := client.ListModels(ctx)
			cancel()

			var options []string
			var modelNames []string

			if listErr == nil {
				for _, m := range localModels {
					size := "cloud"
					if m.Size > 1024*1024 {
						size = fmt.Sprintf("%.1fGB", float64(m.Size)/(1024*1024*1024))
					}
					label := fmt.Sprintf("%-28s [%s]", m.Name, size)
					options = append(options, label)
					modelNames = append(modelNames, m.Name)
				}
			} else {
				fmt.Printf("  %s(Could not fetch local models — showing cloud models only)%s\n", ui.Yellow, ui.Reset)
			}

			// Add cloud models not already in list
			localSet := map[string]bool{}
			for _, name := range modelNames {
				localSet[name] = true
			}
			for _, name := range KnownCloudModels {
				if !localSet[name] {
					label := fmt.Sprintf("%-28s [cloud]", name)
					options = append(options, label)
					modelNames = append(modelNames, name)
				}
			}

			if len(options) == 0 {
				fmt.Printf("  %sNo models available. Pull one with: ollama pull glm-5:cloud%s\n\n", ui.Gray, ui.Reset)
			} else {
				// Limit to 15 options
				if len(options) > 15 {
					options = options[:15]
					modelNames = modelNames[:15]
				}

				fmt.Printf("\n%s%s  Select Model%s %s(current: %s)%s\n", ui.Bold, ui.White, ui.Reset, ui.Gray, cfg.Model, ui.Reset)
				idx := ui.SelectOption(options, 0)
				if idx >= 0 && idx < len(modelNames) {
					cfg.Model = modelNames[idx]
					ag.SetModel(cfg.Model)
					fmt.Printf("  %s[+] Switched to: %s%s\n\n", ui.Green, cfg.Model, ui.Reset)
				}
			}
		} else {
			cfg.Model = parts[1]
			ag.SetModel(cfg.Model)
			fmt.Printf("  %s[+] Switched to: %s%s\n\n", ui.Green, cfg.Model, ui.Reset)
		}

	case "/host":
		if len(parts) < 2 {
			fmt.Printf("  %sCurrent host: %s%s\n\n", ui.Gray, cfg.Host, ui.Reset)
		} else {
			cfg.Host = parts[1]
			ag.SetClient(ollama.NewClient(cfg.Host, cfg.APIKey))
			fmt.Printf("  %s[+] Host updated to: %s%s\n\n", ui.Green, cfg.Host, ui.Reset)
		}

	case "/key":
		if len(parts) < 2 {
			mask := "(not set)"
			if cfg.APIKey != "" && len(cfg.APIKey) > 4 {
				mask = "********" + cfg.APIKey[len(cfg.APIKey)-4:]
			}
			fmt.Printf("  %sCurrent API Key: %s%s\n\n", ui.Gray, mask, ui.Reset)
		} else {
			cfg.APIKey = parts[1]
			ag.SetClient(ollama.NewClient(cfg.Host, cfg.APIKey))
			fmt.Printf("  %s[+] API Key updated (will be saved on /save).%s\n\n", ui.Green, ui.Reset)
		}

	case "/system":
		if len(parts) < 2 {
			prompt := cfg.SystemPrompt
			if prompt == "" {
				prompt = "(using built-in enhanced prompt)"
			} else if len(prompt) > 100 {
				prompt = prompt[:100] + "..."
			}
			fmt.Printf("  %sSystem prompt: %s%s\n\n", ui.Gray, prompt, ui.Reset)
		} else {
			prompt := strings.Join(parts[1:], " ")
			cfg.SystemPrompt = prompt
			ag.SetSystemPrompt(prompt)
			fmt.Printf("  %s[+] System prompt updated.%s\n\n", ui.Green, ui.Reset)
		}

	case "/context":
		msgs := ag.Messages()
		sysTokens, userTokens, asstTokens, toolTokens := 0, 0, 0, 0
		sysCnt, userCnt, asstCnt, toolCnt := 0, 0, 0, 0
		for _, m := range msgs {
			t := tokens.EstimateTokens(m.Content)
			switch m.Role {
			case "system":
				sysTokens += t; sysCnt++
			case "user":
				userTokens += t; userCnt++
			case "assistant":
				asstTokens += t; asstCnt++
			case "tool":
				toolTokens += t; toolCnt++
			}
		}
		total := sysTokens + userTokens + asstTokens + toolTokens
		limit := ag.ContextLimit()
		pct := 0
		if limit > 0 { pct = total * 100 / limit }
		barColor := ui.Green
		if pct > 80 { barColor = ui.Red } else if pct > 50 { barColor = ui.Yellow }
		barW := 30
		filled := pct * barW / 100
		if filled > barW { filled = barW }
		bar := ""
		for i := 0; i < barW; i++ {
			if i < filled { bar += "█" } else { bar += "░" }
		}

		fmt.Printf("\n%s%s  Context Window%s\n", ui.Bold, ui.White, ui.Reset)
		fmt.Printf("  %s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", ui.Gray, ui.Reset)
		fmt.Printf("  %s%-12s  %4d msgs  ~%s tokens%s\n", ui.Purple, "System", sysCnt, tokens.FormatTokenCount(sysTokens), ui.Reset)
		fmt.Printf("  %s%-12s  %4d msgs  ~%s tokens%s\n", ui.Cyan, "User", userCnt, tokens.FormatTokenCount(userTokens), ui.Reset)
		fmt.Printf("  %s%-12s  %4d msgs  ~%s tokens%s\n", ui.Green, "Assistant", asstCnt, tokens.FormatTokenCount(asstTokens), ui.Reset)
		fmt.Printf("  %s%-12s  %4d msgs  ~%s tokens%s\n", ui.Yellow, "Tool", toolCnt, tokens.FormatTokenCount(toolTokens), ui.Reset)
		fmt.Printf("  %s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", ui.Gray, ui.Reset)
		fmt.Printf("  %sTotal:        %4d msgs  ~%s / %s (%d%%)%s\n", ui.White+ui.Bold, sysCnt+userCnt+asstCnt+toolCnt, tokens.FormatTokenCount(total), tokens.FormatTokenCount(limit), pct, ui.Reset)
		fmt.Printf("  %s  %s%s%s%s\n\n", ui.Gray, barColor, bar, ui.Reset, ui.Reset)

	case "/compact":
		if len(parts) >= 2 && parts[1] == "dry-run" {
			targetTokens := ag.ContextLimit() * 60 / 100
			msgs := ag.CompactDryRun(targetTokens)
			if len(msgs) == 0 {
				fmt.Printf("  %sNothing to compact (under 80%% context usage).%s\n\n", ui.Gray, ui.Reset)
			} else {
				fmt.Printf("\n%s%sWould remove %d message(s):%s\n", ui.Bold, ui.White, len(msgs), ui.Reset)
				totalTokens := 0
				for _, m := range msgs {
					t := tokens.EstimateTokens(m.Content)
					totalTokens += t
					content := m.Content
					if len(content) > 50 {
						content = content[:50] + "..."
					}
					content = strings.ReplaceAll(content, "\n", " ")
					fmt.Printf("  %s%-10s%s ~%4d tok  %s%s%s\n", ui.Cyan, m.Role, ui.Reset, t, ui.Gray, content, ui.Reset)
				}
				fmt.Printf("  %sTotal: ~%d tokens would be freed%s\n\n", ui.Yellow, totalTokens, ui.Reset)
			}
		} else {
			removed := ag.Compact(10)
			fmt.Printf("  %s[+] Removed %d old messages.%s\n\n", ui.Green, removed, ui.Reset)
		}

	case "/stats":
		s := ag.Stats()
		dur := s.Duration.Round(time.Second)
		pct := 0
		if s.ContextLimit > 0 {
			pct = s.ContextTokens * 100 / s.ContextLimit
		}

		// Color based on context usage
		barColor := ui.Green
		if pct > 80 {
			barColor = ui.Red
		} else if pct > 50 {
			barColor = ui.Yellow
		}

		// Build progress bar (30 chars wide)
		barWidth := 30
		filled := pct * barWidth / 100
		if filled > barWidth {
			filled = barWidth
		}
		bar := ""
		for i := 0; i < barWidth; i++ {
			if i < filled {
				bar += "█"
			} else {
				bar += "░"
			}
		}

		fmt.Printf("\n%s%s  Session Stats%s\n", ui.Bold, ui.White, ui.Reset)
		fmt.Printf("  %s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", ui.Gray, ui.Reset)
		fmt.Printf("  %sModel:        %s%s%s\n", ui.Gray, ui.Cyan, s.Model, ui.Reset)
		fmt.Printf("  %sDuration:     %s%s%s\n", ui.Gray, ui.White, dur, ui.Reset)
		fmt.Printf("  %sAPI Calls:    %s%d%s\n", ui.Gray, ui.White, s.APICallCount, ui.Reset)
		fmt.Printf("  %sTool Calls:   %s%d%s\n", ui.Gray, ui.White, s.ToolCallCount, ui.Reset)
		fmt.Printf("  %s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", ui.Gray, ui.Reset)
		fmt.Printf("  %sInput Tokens: %s~%s%s\n", ui.Gray, ui.White, tokens.FormatTokenCount(s.InputTokens), ui.Reset)
		fmt.Printf("  %sOutput Tokens:%s~%s%s\n", ui.Gray, ui.White, tokens.FormatTokenCount(s.OutputTokens), ui.Reset)
		fmt.Printf("  %sTotal Tokens: %s~%s%s\n", ui.Gray, ui.White, tokens.FormatTokenCount(s.TotalTokens), ui.Reset)
		fmt.Printf("  %s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", ui.Gray, ui.Reset)
		fmt.Printf("  %sContext:      %s~%s / %s (%d%%)%s\n", ui.Gray, ui.White,
			tokens.FormatTokenCount(s.ContextTokens), tokens.FormatTokenCount(s.ContextLimit), pct, ui.Reset)
		fmt.Printf("  %s  %s%s%s%s\n", ui.Gray, barColor, bar, ui.Reset, ui.Reset)
		if cost, ok := tokens.EstimateCost(s.Model, s.InputTokens, s.OutputTokens); ok {
			fmt.Printf("  %sEst. Cost:    %s~$%.4f%s\n", ui.Gray, ui.Yellow, cost, ui.Reset)
		}
		fmt.Println()

	case "/history":
		count := ag.MessageCount()
		fmt.Printf("  %s%d messages in conversation.%s\n\n", ui.Gray, count, ui.Reset)

	case "/cd":
		if len(parts) < 2 {
			cwd, _ := os.Getwd()
			fmt.Printf("  %s%s%s\n\n", ui.Gray, cwd, ui.Reset)
		} else {
			target := parts[1]
			if strings.HasPrefix(target, "~") {
				home, _ := os.UserHomeDir()
				target = filepath.Join(home, target[1:])
			}
			if err := os.Chdir(target); err != nil {
				fmt.Printf("  %s[-] %v%s\n\n", ui.Red, err, ui.Reset)
			} else {
				cwd, _ := os.Getwd()
				fmt.Printf("  %s[+] %s%s\n\n", ui.Green, cwd, ui.Reset)
			}
		}

	case "/log":
		count := "20"
		if len(parts) > 1 {
			count = parts[1]
		}
		logOut, err := exec.Command("git", "log", "--oneline", "--no-decorate", "-"+count).Output()
		if err != nil {
			fmt.Printf("  %sNot in a git repository or git log failed.%s\n\n", ui.Gray, ui.Reset)
		} else {
			logStr := strings.TrimSpace(string(logOut))
			if logStr == "" {
				fmt.Printf("  %sNo git history found.%s\n\n", ui.Gray, ui.Reset)
			} else {
				fmt.Printf("  %s[+] Injecting git log into conversation...%s\n\n", ui.Green, ui.Reset)
				logPrompt := fmt.Sprintf("Here is the recent git history for this project:\n```\n%s\n```\nPlease review and let me know if you have any observations.", logStr)
				runCtx, runCancel := context.WithCancel(context.Background())
				defer runCancel()
				ag.Run(runCtx, logPrompt)
			}
		}

	case "/diff":
		unstaged, _ := exec.Command("git", "diff").Output()
		staged, _ := exec.Command("git", "diff", "--cached").Output()
		combined := strings.TrimSpace(string(unstaged) + string(staged))
		if combined == "" {
			fmt.Printf("  %sNo uncommitted changes detected.%s\n\n", ui.Gray, ui.Reset)
		} else {
			fmt.Printf("  %s[+] Injecting git diff into conversation (%d bytes)...%s\n\n", ui.Green, len(combined), ui.Reset)
			diffPrompt := fmt.Sprintf("Here are my current uncommitted git changes. Please analyze them:\n```diff\n%s\n```", combined)
			runCtx, runCancel := context.WithCancel(context.Background())
			defer runCancel()
			ag.Run(runCtx, diffPrompt)
		}

	case "/save":
		if err := saveConfig(cfg); err != nil {
			fmt.Printf("  %s[-] Error: %v%s\n\n", ui.Red, err, ui.Reset)
		} else {
			fmt.Printf("  %s[+] Config saved to ~/.g-rump-cli/config.json%s\n\n", ui.Green, ui.Reset)
		}

	case "/undo":
		tracker := ag.Tracker()
		msg, err := tracker.Undo()
		if err != nil {
			fmt.Printf("  %s[-] %v%s\n\n", ui.Yellow, err, ui.Reset)
		} else {
			fmt.Printf("  %s[+] %s%s\n\n", ui.Green, msg, ui.Reset)
		}

	case "/changes":
		tracker := ag.Tracker()
		fmt.Printf("  %s%s%s\n\n", ui.Gray, tracker.Summary(), ui.Reset)

	case "/theme":
		if len(parts) < 2 {
			fmt.Printf("  %sCurrent theme: %s%s%s\n", ui.Gray, ui.Cyan, ui.CurrentTheme, ui.Reset)
			fmt.Printf("  %sAvailable: dark, light%s\n\n", ui.Gray, ui.Reset)
		} else {
			name := strings.ToLower(parts[1])
			if ui.SetTheme(name) {
				cfg.Theme = name
				fmt.Printf("  %s[+] Theme: %s%s\n\n", ui.Green, name, ui.Reset)
			} else {
				fmt.Printf("  %s[-] Unknown theme: %s (try: dark, light)%s\n\n", ui.Red, name, ui.Reset)
			}
		}

	case "/skills":
		available := skills.LoadAll()
		if len(available) == 0 {
			fmt.Printf("  %sNo skills found in ~/.gemini/skills or ~/.claude/skills.%s\n\n", ui.Gray, ui.Reset)
		} else {
			fmt.Printf("\n%s%sAvailable Skills:%s\n", ui.Bold, ui.White, ui.Reset)
			for _, s := range available {
				fmt.Printf("  %s* %s%s\n", ui.Cyan, s.Name, ui.Reset)
			}
			fmt.Printf("\n  %sUse /skill <name> to activate one.%s\n\n", ui.Gray, ui.Reset)
		}

	case "/skill":
		if len(parts) < 2 {
			fmt.Printf("  %sUsage: /skill <name> — load a skill into context%s\n\n", ui.Gray, ui.Reset)
		} else {
			name := parts[1]
			content, err := skills.ReadSkill(name)
			if err != nil {
				fmt.Printf("  %s[-] %v%s\n\n", ui.Red, err, ui.Reset)
			} else {
				ag.SetSkill(name, content)
				fmt.Printf("  %s[+] Skill '%s' activated! Expert instructions added to system prompt.%s\n\n", ui.Green, name, ui.Reset)
			}
		}

	case "/gsd":
		if len(parts) < 2 {
			home, _ := os.UserHomeDir()
			dir := filepath.Join(home, ".claude", "get-shit-done", "workflows")
			entries, err := os.ReadDir(dir)
			if err != nil {
				fmt.Printf("  %s[-] Could not read GSD workflows directory: %v%s\n\n", ui.Red, err, ui.Reset)
				break
			}
			fmt.Printf("\n%s%sAvailable GSD Workflows:%s\n", ui.Bold, ui.White, ui.Reset)
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".md") {
					fmt.Printf("  %s* %s%s\n", ui.Cyan, strings.TrimSuffix(e.Name(), ".md"), ui.Reset)
				}
			}
			fmt.Printf("\n  %sUsage: /gsd <workflow-name>%s\n\n", ui.Gray, ui.Reset)
		} else {
			name := parts[1]
			home, _ := os.UserHomeDir()
			path := filepath.Join(home, ".claude", "get-shit-done", "workflows", name+".md")
			data, err := os.ReadFile(path)
			if err != nil {
				fmt.Printf("  %s[-] GSD workflow '%s' not found: %v%s\n\n", ui.Red, name, err, ui.Reset)
			} else {
				ag.SetSkill("GSD: "+name, string(data))
				fmt.Printf("  %s[+] GSD Workflow '%s' activated! GSD protocols injected into agent context.%s\n\n", ui.Green, name, ui.Reset)
			}
		}

	case "/ralph":
		// First, load the Ralph skill prompt if available
		home, _ := os.UserHomeDir()
		promptPath := filepath.Join(home, ".claude", "ralph", "prompt.md")
		if promptData, err := os.ReadFile(promptPath); err == nil {
			ag.SetSkill("Ralph Agent", string(promptData))
		}

		// Find and display prd.json from current working directory
		cwd, _ := os.Getwd()
		prdPath := filepath.Join(cwd, "ralph", "prd.json")
		prdData, err := os.ReadFile(prdPath)
		if err != nil {
			fmt.Printf("  %s[-] No ralph/prd.json found in %s%s\n", ui.Red, cwd, ui.Reset)
			fmt.Printf("  %sCreate one at ralph/prd.json to get started.%s\n\n", ui.Gray, ui.Reset)
			break
		}

		// Parse the PRD
		var prd struct {
			Project     string `json:"project"`
			BranchName  string `json:"branchName"`
			Description string `json:"description"`
			UserStories []struct {
				ID       string   `json:"id"`
				Title    string   `json:"title"`
				Passes   bool     `json:"passes"`
				Notes    string   `json:"notes"`
				AcceptanceCriteria []string `json:"acceptanceCriteria"`
				Description string `json:"description"`
			} `json:"userStories"`
		}
		if err := json.Unmarshal(prdData, &prd); err != nil {
			fmt.Printf("  %s[-] Failed to parse prd.json: %v%s\n\n", ui.Red, err, ui.Reset)
			break
		}

		// Display progress
		passed, total := 0, len(prd.UserStories)
		for _, s := range prd.UserStories {
			if s.Passes {
				passed++
			}
		}

		fmt.Printf("\n  %s%s%s%s\n", ui.Bold, ui.Purple, prd.Project, ui.Reset)
		fmt.Printf("  %s%s%s\n", ui.Gray, prd.Description, ui.Reset)
		fmt.Printf("  %sBranch: %s%s\n\n", ui.Cyan, prd.BranchName, ui.Reset)
		fmt.Printf("  %sProgress: %s%d/%d stories passing%s\n\n", ui.Bold, ui.Green, passed, total, ui.Reset)

		for _, s := range prd.UserStories {
			status := fmt.Sprintf("%s[PASS]%s", ui.Green, ui.Reset)
			if !s.Passes {
				status = fmt.Sprintf("%s[FAIL]%s", ui.Red, ui.Reset)
			}
			fmt.Printf("  %s %s%s%s %s\n", status, ui.Bold, s.ID, ui.Reset, s.Title)
		}
		fmt.Println()

		// If all pass, celebrate
		if passed == total {
			fmt.Printf("  %s%sAll stories passing! Run complete.%s\n\n", ui.Bold, ui.Green, ui.Reset)
			break
		}

		// Find next failing story and offer to execute
		extraArgs := strings.TrimSpace(strings.TrimPrefix(input, "/ralph"))
		if extraArgs == "" {
			fmt.Printf("  %sTip: /ralph run — execute the next failing story%s\n", ui.Gray, ui.Reset)
			fmt.Printf("  %s     /ralph run US-003 — execute a specific story%s\n\n", ui.Gray, ui.Reset)
			break
		}

		if strings.HasPrefix(extraArgs, "run") {
			targetID := strings.TrimSpace(strings.TrimPrefix(extraArgs, "run"))
			var target *struct {
				ID       string   `json:"id"`
				Title    string   `json:"title"`
				Passes   bool     `json:"passes"`
				Notes    string   `json:"notes"`
				AcceptanceCriteria []string `json:"acceptanceCriteria"`
				Description string `json:"description"`
			}

			if targetID != "" {
				// Find specific story
				for i := range prd.UserStories {
					if strings.EqualFold(prd.UserStories[i].ID, targetID) {
						target = &prd.UserStories[i]
						break
					}
				}
				if target == nil {
					fmt.Printf("  %s[-] Story %s not found%s\n\n", ui.Red, targetID, ui.Reset)
					break
				}
			} else {
				// Find first failing story
				for i := range prd.UserStories {
					if !prd.UserStories[i].Passes {
						target = &prd.UserStories[i]
						break
					}
				}
			}

			if target == nil {
				fmt.Printf("  %sAll stories passing!%s\n\n", ui.Green, ui.Reset)
				break
			}

			fmt.Printf("  %s[>] Executing %s: %s%s\n\n", ui.Cyan+ui.Bold, target.ID, target.Title, ui.Reset)

			// Build execution prompt
			criteria := strings.Join(target.AcceptanceCriteria, "\n- ")
			execPrompt := fmt.Sprintf(
				"Execute this user story from the project PRD:\n\n**%s: %s**\n\n%s\n\n**Acceptance Criteria:**\n- %s\n\nIMPORTANT: Implement ALL acceptance criteria. Read relevant files first, then make surgical changes. Verify with typecheck (go build ./...) before reporting done.",
				target.ID, target.Title, target.Description, criteria,
			)

			runCtx, runCancel := context.WithCancel(context.Background())
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt)
			go func() {
				select {
				case <-sigCh:
					runCancel()
				case <-runCtx.Done():
				}
			}()

			if err := ag.Run(runCtx, execPrompt); err != nil {
				if err == context.Canceled || strings.Contains(err.Error(), "context canceled") {
					fmt.Printf("\n%s%s[interrupted]%s\n\n", ui.Dim, ui.Yellow, ui.Reset)
				} else {
					fmt.Printf("\n%s%sError: %v%s\n\n", ui.Bold, ui.Red, err, ui.Reset)
				}
			}

			signal.Stop(sigCh)
			signal.Reset(os.Interrupt)
			runCancel()
		}

	case "/improve":
		improvePrompt := strings.Join(parts[1:], " ")
		if improvePrompt == "" {
			fmt.Printf("  %sUsage: /improve <what to improve>%s\n", ui.Gray, ui.Reset)
			fmt.Printf("  %sExample: /improve add a /test command that runs go test%s\n\n", ui.Gray, ui.Reset)
		} else {
			fmt.Printf("  %s[+] Engaging self-improvement protocol...%s\n\n", ui.Green, ui.Reset)
			selfEditContext := fmt.Sprintf(`CRITICAL DIRECTIVE: You are modifying your OWN source code.
Your source code is located in the current workspace.
You must use your tools (grep, list_directory, read_file) to find the relevant code in this repository, then carefully use edit_file or write_file to implement the user's request.
After modifying the code, you MUST run 'go build -o g-rump-cli' and verify it compiles, then run 'sudo mv g-rump-cli /usr/local/bin/g-rump-cli' using bash to deploy the update.

User's Request: %s`, improvePrompt)
			runCtx, runCancel := context.WithCancel(context.Background())
			defer runCancel()
			ag.Run(runCtx, selfEditContext)
		}

	case "/clearskill":
		ag.ClearSkill()
		fmt.Printf("  %s[+] Active skill cleared.%s\n\n", ui.Green, ui.Reset)

	case "/install":
		if err := createDesktopFolder(); err != nil {
			fmt.Printf("  %s[-] %v%s\n\n", ui.Red, err, ui.Reset)
		} else {
			fmt.Printf("  %s[+] Created ~/Desktop/G-Rump-CLI/%s\n\n", ui.Green, ui.Reset)
		}

	case "/paste":
		var clipCmd *exec.Cmd
		if runtime.GOOS == "darwin" {
			clipCmd = exec.Command("pbpaste")
		} else {
			clipCmd = exec.Command("xclip", "-selection", "clipboard", "-o")
		}
		clipOut, err := clipCmd.Output()
		if err != nil {
			// Fallback for Linux
			if runtime.GOOS != "darwin" {
				clipCmd = exec.Command("xsel", "--clipboard", "--output")
				clipOut, err = clipCmd.Output()
			}
			if err != nil {
				fmt.Printf("  %s[-] Could not read clipboard: %v%s\n\n", ui.Red, err, ui.Reset)
				break
			}
		}
		clipContent := strings.TrimSpace(string(clipOut))
		if clipContent == "" {
			fmt.Printf("  %sClipboard is empty.%s\n\n", ui.Gray, ui.Reset)
		} else {
			lines := strings.Split(clipContent, "\n")
			preview := clipContent
			if len(lines) > 3 {
				preview = strings.Join(lines[:3], "\n") + fmt.Sprintf("\n... (%d total lines)", len(lines))
			}
			fmt.Printf("  %s[+] Clipboard content:%s\n  %s%s%s\n\n", ui.Green, ui.Reset, ui.Dim, preview, ui.Reset)
			runCtx, runCancel := context.WithCancel(context.Background())
			defer runCancel()
			ag.Run(runCtx, clipContent)
		}

	case "/export":
		exportName := ""
		if len(parts) > 1 {
			exportName = strings.Join(parts[1:], " ")
		}
		if exportName == "" {
			exportName = fmt.Sprintf("conversation-%d", time.Now().Unix())
		}
		if !strings.HasSuffix(exportName, ".md") {
			exportName += ".md"
		}
		exportDir := filepath.Join(configDir(), "exports")
		os.MkdirAll(exportDir, 0755)
		exportPath := filepath.Join(exportDir, exportName)

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# G-Rump-CLI Conversation\n\n**Model:** %s\n**Date:** %s\n\n---\n\n", cfg.Model, time.Now().Format("2006-01-02 15:04:05")))
		for _, m := range ag.Messages() {
			switch m.Role {
			case "system":
				continue
			case "user":
				sb.WriteString(fmt.Sprintf("## User\n\n%s\n\n", m.Content))
			case "assistant":
				sb.WriteString(fmt.Sprintf("## Assistant\n\n%s\n\n", m.Content))
			case "tool":
				sb.WriteString(fmt.Sprintf("### Tool Result\n\n```\n%s\n```\n\n", m.Content))
			}
		}

		if err := os.WriteFile(exportPath, []byte(sb.String()), 0644); err != nil {
			fmt.Printf("  %s[-] Export failed: %v%s\n\n", ui.Red, err, ui.Reset)
		} else {
			fmt.Printf("  %s[+] Exported to: %s%s\n\n", ui.Green, exportPath, ui.Reset)
		}

	case "/sessions":
		if len(parts) >= 2 && parts[1] == "gc" {
			maxAge := 30
			if len(parts) >= 3 {
				if n, err := fmt.Sscanf(parts[2], "%d", &maxAge); n != 1 || err != nil || maxAge < 1 {
					fmt.Printf("  %s[-] Invalid age. Provide a positive number of days.%s\n\n", ui.Red, ui.Reset)
					return false
				}
			}
			deleted, freed, err := session.GarbageCollect(maxAge)
			if err != nil {
				fmt.Printf("  %s[-] GC failed: %v%s\n\n", ui.Red, err, ui.Reset)
			} else if deleted == 0 {
				fmt.Printf("  %sNo sessions older than %d days.%s\n\n", ui.Gray, maxAge, ui.Reset)
			} else {
				fmt.Printf("  %s[+] Deleted %d session(s), freed %s%s\n\n", ui.Green, deleted, formatBytes(freed), ui.Reset)
			}
		} else {
			infos := session.List()
			if len(infos) == 0 {
				fmt.Printf("  %sNo saved sessions.%s\n\n", ui.Gray, ui.Reset)
			} else {
				fmt.Printf("\n%s%sSaved Sessions:%s\n", ui.Bold, ui.White, ui.Reset)
				for _, info := range infos {
					name := info.Name
					if name == "" {
						name = info.ID[:8]
					}
					ago := time.Since(info.Updated)
					agoStr := formatDuration(ago)
					fmt.Printf("  %s%-20s%s %s%s (%d messages)%s\n",
						ui.Cyan, name, ui.Reset, ui.Gray, agoStr, info.Messages, ui.Reset)
				}
				fmt.Println()
			}
		}

	case "/session":
		if len(parts) < 2 {
			fmt.Printf("  %sUsage: /session <name> — save current conversation%s\n\n", ui.Gray, ui.Reset)
		} else {
			name := strings.Join(parts[1:], " ")
			id := fmt.Sprintf("%d", time.Now().UnixNano())
			sess := session.Session{
				ID:       id,
				Name:     name,
				Model:    cfg.Model,
				Messages: ag.Messages(),
			}
			if err := session.Save(sess); err != nil {
				fmt.Printf("  %s✗ Error: %v%s\n\n", ui.Red, err, ui.Reset)
			} else {
				*sessionID = id
				fmt.Printf("  %s✓ Session saved: %s%s\n\n", ui.Green, name, ui.Reset)
			}
		}

	case "/resume":
		if len(parts) < 2 {
			fmt.Printf("  %sUsage: /resume <name or id> — resume a saved session%s\n\n", ui.Gray, ui.Reset)
		} else {
			query := strings.Join(parts[1:], " ")
			sess := findSession(query)
			if sess == nil {
				fmt.Printf("  %s✗ Session not found: %s%s\n\n", ui.Red, query, ui.Reset)
			} else {
				ag.LoadMessages(sess.Messages)
				*sessionID = sess.ID
				if sess.Model != "" {
					cfg.Model = sess.Model
					ag.SetModel(sess.Model)
				}
				name := sess.Name
				if name == "" {
					name = sess.ID[:8]
				}
				count := 0
				for _, m := range sess.Messages {
					if m.Role == "user" {
						count++
					}
				}
				fmt.Printf("  %s✓ Resumed: %s (%d messages)%s\n\n", ui.Green, name, count, ui.Reset)
			}
		}

	case "/search":
		if len(parts) < 2 {
			fmt.Printf("  %sUsage: /search <query>%s\n\n", ui.Gray, ui.Reset)
		} else {
			query := strings.ToLower(strings.Join(parts[1:], " "))
			msgs := ag.Messages()
			type match struct {
				idx     int
				role    string
				snippet string
			}
			var matches []match
			for i, m := range msgs {
				if m.Role == "system" {
					continue
				}
				lower := strings.ToLower(m.Content)
				pos := strings.Index(lower, query)
				if pos >= 0 {
					// Extract snippet with 50 chars context on each side
					start := pos - 50
					if start < 0 {
						start = 0
					}
					end := pos + len(query) + 50
					if end > len(m.Content) {
						end = len(m.Content)
					}
					snippet := m.Content[start:end]
					snippet = strings.ReplaceAll(snippet, "\n", " ")
					matches = append(matches, match{idx: i, role: m.Role, snippet: snippet})
				}
			}
			if len(matches) == 0 {
				fmt.Printf("  %sNo matches found in conversation.%s\n\n", ui.Gray, ui.Reset)
			} else {
				// Show last 10
				start := 0
				if len(matches) > 10 {
					start = len(matches) - 10
				}
				fmt.Printf("\n%s%s  Search Results (%d matches)%s\n", ui.Bold, ui.White, len(matches), ui.Reset)
				for _, m := range matches[start:] {
					roleColor := ui.Gray
					switch m.role {
					case "user":
						roleColor = ui.Cyan
					case "assistant":
						roleColor = ui.Purple
					case "tool":
						roleColor = ui.Yellow
					}
					fmt.Printf("  %s[%d]%s %s%s%s: ...%s...\n", ui.Gray, m.idx, ui.Reset, roleColor, m.role, ui.Reset, m.snippet)
				}
				fmt.Println()
			}
		}

	case "/drop":
		if len(parts) < 2 {
			fmt.Printf("  %sUsage: /drop N or /drop N-M%s\n\n", ui.Gray, ui.Reset)
		} else {
			msgs := ag.Messages()
			arg := parts[1]
			var start, end int

			if strings.Contains(arg, "-") {
				rangeParts := strings.SplitN(arg, "-", 2)
				fmt.Sscanf(rangeParts[0], "%d", &start)
				fmt.Sscanf(rangeParts[1], "%d", &end)
			} else {
				fmt.Sscanf(arg, "%d", &start)
				end = start
			}

			// Validate range
			sysOffset := 0
			if len(msgs) > 0 && msgs[0].Role == "system" {
				sysOffset = 1
			}
			if start < sysOffset || end >= len(msgs) || start > end {
				fmt.Printf("  %s[-] Invalid range. Valid indices: %d-%d%s\n\n", ui.Red, sysOffset, len(msgs)-1, ui.Reset)
			} else {
				// Estimate tokens being freed
				freed := 0
				for i := start; i <= end; i++ {
					freed += tokens.EstimateTokens(msgs[i].Content)
				}
				// Remove messages in range
				newMsgs := make([]ollama.Message, 0, len(msgs)-(end-start+1))
				newMsgs = append(newMsgs, msgs[:start]...)
				newMsgs = append(newMsgs, msgs[end+1:]...)
				ag.LoadMessages(newMsgs)
				count := end - start + 1
				fmt.Printf("  %s[+] Dropped %d message(s). Context freed: ~%s tokens%s\n\n",
					ui.Green, count, tokens.FormatTokenCount(freed), ui.Reset)
			}
		}

	case "/tokens":
		msgs := ag.Messages()
		total := 0
		type msgInfo struct {
			idx    int
			role   string
			toks   int
			preview string
		}
		var infos []msgInfo
		for i, m := range msgs {
			t := tokens.EstimateTokens(m.Content)
			total += t
			preview := m.Content
			if len(preview) > 60 {
				preview = preview[:60] + "..."
			}
			preview = strings.ReplaceAll(preview, "\n", " ")
			infos = append(infos, msgInfo{idx: i, role: m.Role, toks: t, preview: preview})
		}

		fmt.Printf("\n%s%s  Token Breakdown%s\n", ui.Bold, ui.White, ui.Reset)
		fmt.Printf("  %s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", ui.Gray, ui.Reset)
		for _, info := range infos {
			roleColor := ui.Gray
			switch info.role {
			case "user":
				roleColor = ui.Cyan
			case "assistant":
				roleColor = ui.Purple
			case "tool":
				roleColor = ui.Yellow
			case "system":
				roleColor = ui.Red
			}
			tokColor := ui.Gray
			if info.toks > 1000 {
				tokColor = ui.Yellow
			}
			fmt.Printf("  %s[%d]%s %s%-10s%s %s%5d tok%s  %s%s%s\n",
				ui.Gray, info.idx, ui.Reset,
				roleColor, info.role, ui.Reset,
				tokColor, info.toks, ui.Reset,
				ui.Dim, info.preview, ui.Reset)
		}
		s := ag.Stats()
		pct := 0
		if s.ContextLimit > 0 {
			pct = total * 100 / s.ContextLimit
		}
		fmt.Printf("  %s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", ui.Gray, ui.Reset)
		fmt.Printf("  %sTotal: ~%s tokens (%d%% of %s limit)%s\n\n",
			ui.White, tokens.FormatTokenCount(total), pct, tokens.FormatTokenCount(s.ContextLimit), ui.Reset)

	case "/retry":
		msgs := ag.Messages()
		// Find last user message
		lastUserIdx := -1
		lastUserMsg := ""
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "user" {
				lastUserIdx = i
				lastUserMsg = msgs[i].Content
				break
			}
		}
		if lastUserIdx < 0 {
			fmt.Printf("  %sNo previous message to retry.%s\n\n", ui.Gray, ui.Reset)
		} else {
			// Remove everything from the last user message onward
			ag.LoadMessages(msgs[:lastUserIdx])
			fmt.Printf("  %s[+] Retrying: %s%s\n\n", ui.Green, lastUserMsg[:min(len(lastUserMsg), 80)], ui.Reset)
			runCtx, runCancel := context.WithCancel(context.Background())
			defer runCancel()
			ag.Run(runCtx, lastUserMsg)
		}

	case "/commit":
		// Get staged diff first
		stagedOut, _ := exec.Command("git", "diff", "--cached").Output()
		diff := strings.TrimSpace(string(stagedOut))
		if diff == "" {
			// Nothing staged, stage all changes
			exec.Command("git", "add", "-A").Run()
			stagedOut, _ = exec.Command("git", "diff", "--cached").Output()
			diff = strings.TrimSpace(string(stagedOut))
		}
		if diff == "" {
			fmt.Printf("  %sNo changes to commit.%s\n\n", ui.Gray, ui.Reset)
		} else {
			fmt.Printf("  %s[+] Generating commit message...%s\n", ui.Green, ui.Reset)
			commitPrompt := fmt.Sprintf("Generate a concise, conventional commit message (type: subject format) for these changes. Output ONLY the commit message, nothing else:\n```diff\n%s\n```", diff)
			runCtx, runCancel := context.WithCancel(context.Background())
			defer runCancel()
			// Capture the agent's response by running it
			ag.Run(runCtx, commitPrompt)
			// Extract the last assistant message as the commit message
			msgs := ag.Messages()
			commitMsg := ""
			for i := len(msgs) - 1; i >= 0; i-- {
				if msgs[i].Role == "assistant" && msgs[i].Content != "" {
					commitMsg = strings.TrimSpace(msgs[i].Content)
					break
				}
			}
			if commitMsg != "" {
				// Clean up: remove backticks or markdown formatting
				commitMsg = strings.Trim(commitMsg, "`\"'")
				commitMsg = strings.TrimSpace(commitMsg)
				out, err := exec.Command("git", "commit", "-m", commitMsg).CombinedOutput()
				if err != nil {
					fmt.Printf("  %s[-] Commit failed: %s%s\n\n", ui.Red, strings.TrimSpace(string(out)), ui.Reset)
				} else {
					fmt.Printf("  %s%s%s\n\n", ui.Green, strings.TrimSpace(string(out)), ui.Reset)
				}
			}
		}

	case "/blame":
		if len(parts) < 2 {
			fmt.Printf("  %sUsage: /blame <file>%s\n\n", ui.Gray, ui.Reset)
		} else {
			blamePath := parts[1]
			blameOut, err := exec.Command("git", "blame", "--line-porcelain", blamePath).CombinedOutput()
			if err != nil {
				errStr := strings.TrimSpace(string(blameOut))
				if strings.Contains(errStr, "not a git repository") {
					fmt.Printf("  %s[-] Not in a git repository.%s\n\n", ui.Red, ui.Reset)
				} else if strings.Contains(errStr, "no such path") || strings.Contains(errStr, "fatal") {
					fmt.Printf("  %s[-] File not found or git error: %s%s\n\n", ui.Red, errStr, ui.Reset)
				} else {
					fmt.Printf("  %s[-] git blame failed: %s%s\n\n", ui.Red, errStr, ui.Reset)
				}
			} else {
				// Parse porcelain output into readable summary
				type blameEntry struct {
					author string
					date   string
					summary string
					lines  int
				}
				authorLines := map[string]int{}
				var entries []blameEntry
				blameLines := strings.Split(string(blameOut), "\n")
				var curAuthor, curDate, curSummary string
				for _, bl := range blameLines {
					if strings.HasPrefix(bl, "author ") {
						curAuthor = strings.TrimPrefix(bl, "author ")
					} else if strings.HasPrefix(bl, "author-time ") {
						// Unix timestamp - just keep raw for simplicity
						curDate = strings.TrimPrefix(bl, "author-time ")
					} else if strings.HasPrefix(bl, "summary ") {
						curSummary = strings.TrimPrefix(bl, "summary ")
					} else if strings.HasPrefix(bl, "\t") {
						// This is the actual line content - means we have a complete entry
						authorLines[curAuthor]++
						entries = append(entries, blameEntry{author: curAuthor, date: curDate, summary: curSummary, lines: 1})
					}
				}

				var blameSummary string
				if len(entries) > 100 {
					// Summarize by author
					var summaryLines []string
					summaryLines = append(summaryLines, fmt.Sprintf("File: %s (%d lines)", blamePath, len(entries)))
					summaryLines = append(summaryLines, "")
					summaryLines = append(summaryLines, "Contributors:")
					for author, count := range authorLines {
						pct := float64(count) * 100.0 / float64(len(entries))
						summaryLines = append(summaryLines, fmt.Sprintf("  %-30s %4d lines (%5.1f%%)", author, count, pct))
					}
					blameSummary = strings.Join(summaryLines, "\n")
				} else {
					// Show detailed blame
					var summaryLines []string
					summaryLines = append(summaryLines, fmt.Sprintf("File: %s (%d lines)", blamePath, len(entries)))
					summaryLines = append(summaryLines, "")
					for author, count := range authorLines {
						pct := float64(count) * 100.0 / float64(len(entries))
						summaryLines = append(summaryLines, fmt.Sprintf("  %-30s %4d lines (%5.1f%%)", author, count, pct))
					}
					blameSummary = strings.Join(summaryLines, "\n")
				}

				fmt.Printf("  %s[+] Injecting git blame for %s into conversation...%s\n\n", ui.Green, blamePath, ui.Reset)
				blamePrompt := fmt.Sprintf("Here is the git blame for %s:\n```\n%s\n```", blamePath, blameSummary)
				runCtx, runCancel := context.WithCancel(context.Background())
				defer runCancel()
				ag.Run(runCtx, blamePrompt)
			}
		}

	case "/env":
		inject := false
		if len(parts) > 1 && parts[1] == "--inject" {
			inject = true
		}

		type envCheck struct {
			name    string
			cmd     string
			args    []string
		}
		checks := []envCheck{
			{"Go", "go", []string{"version"}},
			{"Node", "node", []string{"-v"}},
			{"Python", "python3", []string{"--version"}},
			{"Rust", "rustc", []string{"--version"}},
			{"Docker", "docker", []string{"--version"}},
		}

		var envLines []string
		envLines = append(envLines, fmt.Sprintf("  OS:           %s/%s", runtime.GOOS, runtime.GOARCH))
		envLines = append(envLines, fmt.Sprintf("  Shell:        %s", os.Getenv("SHELL")))

		for _, chk := range checks {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			out, err := exec.CommandContext(ctx, chk.cmd, chk.args...).Output()
			cancel()
			ver := "not installed"
			if err == nil {
				ver = strings.TrimSpace(string(out))
			}
			envLines = append(envLines, fmt.Sprintf("  %-12s  %s", chk.name+":", ver))
		}

		cwd, _ := os.Getwd()
		envLines = append(envLines, fmt.Sprintf("  CWD:          %s", cwd))

		// Check for common files
		for _, f := range []string{".env", "Dockerfile", "docker-compose.yml"} {
			present := "no"
			if _, err := os.Stat(filepath.Join(cwd, f)); err == nil {
				present = "yes"
			}
			envLines = append(envLines, fmt.Sprintf("  %-12s  %s", f+":", present))
		}

		envOutput := strings.Join(envLines, "\n")
		fmt.Printf("\n%s%s  Environment Info%s\n", ui.Bold, ui.White, ui.Reset)
		fmt.Printf("  %s━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━%s\n", ui.Gray, ui.Reset)
		fmt.Printf("%s%s%s\n\n", ui.Gray, envOutput, ui.Reset)

		if inject {
			fmt.Printf("  %s[+] Injecting environment info into conversation...%s\n\n", ui.Green, ui.Reset)
			envPrompt := fmt.Sprintf("Here is my development environment info:\n```\n%s\n```\nUse this context when helping me.", envOutput)
			runCtx, runCancel := context.WithCancel(context.Background())
			defer runCancel()
			ag.Run(runCtx, envPrompt)
		}

	case "/alias":
		if cfg.Aliases == nil {
			cfg.Aliases = make(map[string]string)
		}
		if len(parts) < 2 {
			// List all aliases
			if len(cfg.Aliases) == 0 {
				fmt.Printf("  %sNo aliases defined. Use /alias name=/command to create one.%s\n\n", ui.Gray, ui.Reset)
			} else {
				fmt.Printf("\n%s%s  Aliases%s\n", ui.Bold, ui.White, ui.Reset)
				for k, v := range cfg.Aliases {
					fmt.Printf("  %s/%s%s -> %s%s%s\n", ui.Cyan, k, ui.Reset, ui.Gray, v, ui.Reset)
				}
				fmt.Println()
			}
		} else {
			aliasArg := strings.Join(parts[1:], " ")
			eqIdx := strings.Index(aliasArg, "=")
			if eqIdx < 0 {
				fmt.Printf("  %sUsage: /alias name=/command  or  /alias name=  (to delete)%s\n\n", ui.Gray, ui.Reset)
			} else {
				aliasName := strings.TrimSpace(aliasArg[:eqIdx])
				aliasValue := strings.TrimSpace(aliasArg[eqIdx+1:])
				if aliasValue == "" {
					// Delete alias
					delete(cfg.Aliases, aliasName)
					saveConfig(cfg)
					fmt.Printf("  %s[+] Alias '/%s' deleted.%s\n\n", ui.Green, aliasName, ui.Reset)
				} else {
					cfg.Aliases[aliasName] = aliasValue
					saveConfig(cfg)
					fmt.Printf("  %s[+] Alias '/%s' -> '%s'%s\n\n", ui.Green, aliasName, aliasValue, ui.Reset)
				}
			}
		}

	case "/stash":
		subCmd := ""
		if len(parts) > 1 {
			subCmd = strings.ToLower(parts[1])
		}
		switch subCmd {
		case "pop":
			out, err := exec.Command("git", "stash", "pop").CombinedOutput()
			outStr := strings.TrimSpace(string(out))
			if err != nil {
				fmt.Printf("  %s[-] Stash pop failed: %s%s\n\n", ui.Red, outStr, ui.Reset)
			} else {
				fmt.Printf("  %s[+] %s%s\n\n", ui.Green, outStr, ui.Reset)
			}
		case "list":
			out, err := exec.Command("git", "stash", "list").CombinedOutput()
			outStr := strings.TrimSpace(string(out))
			if err != nil {
				fmt.Printf("  %s[-] Not in a git repository or stash list failed.%s\n\n", ui.Red, ui.Reset)
			} else if outStr == "" {
				fmt.Printf("  %sNo stashes found.%s\n\n", ui.Gray, ui.Reset)
			} else {
				fmt.Printf("\n%s%s  Git Stashes%s\n", ui.Bold, ui.White, ui.Reset)
				for _, line := range strings.Split(outStr, "\n") {
					fmt.Printf("  %s%s%s\n", ui.Gray, line, ui.Reset)
				}
				fmt.Println()
			}
		default:
			// Default: stash push
			out, err := exec.Command("git", "stash", "push", "-m", "G-Rump-CLI stash").CombinedOutput()
			outStr := strings.TrimSpace(string(out))
			if err != nil {
				if strings.Contains(outStr, "not a git repository") {
					fmt.Printf("  %s[-] Not in a git repository.%s\n\n", ui.Red, ui.Reset)
				} else if strings.Contains(outStr, "No local changes") {
					fmt.Printf("  %sNothing to stash.%s\n\n", ui.Gray, ui.Reset)
				} else {
					fmt.Printf("  %s[-] Stash failed: %s%s\n\n", ui.Red, outStr, ui.Reset)
				}
			} else {
				fmt.Printf("  %s[+] %s%s\n\n", ui.Green, outStr, ui.Reset)
			}
		}

	case "/checkpoint":
		if len(parts) < 2 {
			cps := ag.Checkpoints()
			if len(cps) == 0 {
				fmt.Printf("  %sNo checkpoints saved. Usage: /checkpoint <name>%s\n\n", ui.Gray, ui.Reset)
			} else {
				fmt.Printf("\n%s%sSaved Checkpoints:%s\n", ui.Bold, ui.White, ui.Reset)
				for name, count := range cps {
					est := count * 50 // rough estimate
					fmt.Printf("  %s%-20s%s %s%d messages (~%s tokens)%s\n",
						ui.Cyan, name, ui.Reset, ui.Gray, count, tokens.FormatTokenCount(est), ui.Reset)
				}
				fmt.Println()
			}
		} else {
			name := parts[1]
			ag.SaveCheckpoint(name)
			fmt.Printf("  %s[+] Checkpoint '%s' saved (%d messages)%s\n\n", ui.Green, name, len(ag.Messages()), ui.Reset)
		}

	case "/restore":
		if len(parts) < 2 {
			fmt.Printf("  %sUsage: /restore <checkpoint-name>%s\n\n", ui.Gray, ui.Reset)
		} else {
			name := parts[1]
			if ag.RestoreCheckpoint(name) {
				fmt.Printf("  %s[+] Restored checkpoint '%s' (%d messages)%s\n\n", ui.Green, name, len(ag.Messages()), ui.Reset)
			} else {
				fmt.Printf("  %s[-] Checkpoint '%s' not found%s\n\n", ui.Red, name, ui.Reset)
			}
		}

	case "/files":
		touched := ag.TouchedFiles()
		if len(touched) == 0 {
			fmt.Printf("  %sNo files touched this session.%s\n\n", ui.Gray, ui.Reset)
		} else {
			paths := make([]string, 0, len(touched))
			for p := range touched {
				paths = append(paths, p)
			}
			sort.Strings(paths)
			fmt.Printf("\n%s%sFiles touched this session:%s\n", ui.Bold, ui.White, ui.Reset)
			for _, p := range paths {
				action := touched[p]
				var color string
				switch action {
				case "read":
					color = ui.Gray
				case "write":
					color = ui.Green
				case "edit":
					color = ui.Yellow
				default:
					color = ui.Gray
				}
				fmt.Printf("  %s%-6s%s %s\n", color, action, ui.Reset, p)
			}
			fmt.Println()
		}

	case "/help":
		fmt.Printf("\n%s%sCommands:%s\n", ui.Bold, ui.White, ui.Reset)
		cmds := []struct{ cmd, desc string }{
			{"/clear", "Clear conversation history"},
			{"/compact", "Trim old messages to save context"},
			{"/model", "Show or switch model (/model llama3.3)"},
			{"/host", "Set the API host (e.g. https://openrouter.ai/api/v1)"},
			{"/key", "Set your API key for cloud providers"},
			{"/system", "Show or set system prompt"},
			{"/mode", "Cycle modes (Normal -> Plan -> Deep)"},
			{"/init", "Initialize .bu local memory in the current directory"},
			{"/plan", "Toggle Plan Mode (read-only architectural planning)"},
			{"/deep", "Toggle Deep Thinking Protocol (forces extreme, complex analysis)"},
			{"/accept", "Toggle Accept Edits Mode (prompt before modifying files)"},
			{"/render", "Toggle Render Mode (rich markdown output with syntax highlighting)"},
			{"/permissions", "Show tool permission status (auto/always/ask)"},
			{"/theme", "Switch theme (/theme dark or /theme light)"},
			{"/skills", "List available expert skills"},
			{"/skill", "Activate a skill (/skill <name>)"},
			{"/gsd", "List or activate a Get-Shit-Done workflow"},
			{"/ralph", "Activate the Ralph autonomous coding agent"},
			{"/clearskill", "Clear the currently active skill/workflow"},
			{"/stats", "Show session metrics (tokens, API calls, context usage)"},
			{"/context-limit", "Set context window token limit (e.g., /context-limit 32000)"},
			{"/max-turns", "Set max agent loop iterations (e.g., /max-turns 50)"},
			{"/history", "Show message count"},
			{"/cd", "Show or change working directory"},
			{"/log", "Inject recent git history into conversation (/log 20)"},
			{"/search", "Search conversation history (/search <query>)"},
			{"/drop", "Remove messages from context (/drop N or /drop N-M)"},
			{"/tokens", "Show per-message token breakdown"},
			{"/retry", "Re-send your last message (removes previous response)"},
			{"/commit", "Auto-generate a commit message from staged/all changes"},
			{"/diff", "Inject current git diff into conversation for analysis"},
			{"/undo", "Undo the last file change"},
			{"/changes", "Show file changes this session"},
			{"/paste", "Paste clipboard contents into conversation"},
			{"/export", "Export conversation as markdown (/export my-notes)"},
			{"/session", "Save current session (/session my-task)"},
			{"/sessions", "List saved sessions"},
			{"/resume", "Resume a saved session (/resume my-task)"},
			{"/improve", "Command the agent to improve its own source code"},
			{"/install", "Create ~/Desktop/G-Rump-CLI/ shortcut"},
			{"/blame", "Inject git blame summary into conversation (/blame <file>)"},
			{"/env", "Show development environment info (/env --inject)"},
			{"/alias", "Manage command aliases (/alias name=/command)"},
			{"/stash", "Git stash operations (/stash, /stash pop, /stash list)"},
			{"/files", "Show all files touched this session"},
			{"/checkpoint", "Save conversation snapshot (/checkpoint <name>)"},
			{"/restore", "Restore a checkpoint (/restore <name>)"},
			{"/save", "Save settings to ~/.g-rump-cli/config.json"},
			{"/help", "Show this help"},
			{"/exit", "Exit G-Rump-CLI"},
		}
		for _, c := range cmds {
			fmt.Printf("  %s%-14s%s %s%s%s\n", ui.Cyan, c.cmd, ui.Reset, ui.Gray, c.desc, ui.Reset)
		}
		fmt.Printf("\n%s%sTools (used by the AI):%s\n", ui.Bold, ui.White, ui.Reset)
		tls := []struct{ name, desc string }{
			{"read_file", "Read file contents with line numbers"},
			{"write_file", "Create or overwrite files"},
			{"edit_file", "Find-and-replace edits"},
			{"bash", "Run shell commands"},
			{"glob", "Find files by pattern"},
			{"grep", "Search file contents"},
			{"list_directory", "List directory contents"},
		}
		for _, t := range tls {
			fmt.Printf("  %s%-16s%s %s%s%s\n", ui.Purple, t.name, ui.Reset, ui.Gray, t.desc, ui.Reset)
		}
		fmt.Printf("\n%s%sTips:%s\n", ui.Bold, ui.White, ui.Reset)
		fmt.Printf("  %s• Type \"\"\" for multi-line input mode%s\n", ui.Gray, ui.Reset)
		fmt.Printf("  %s• End a line with \\ to continue on next line%s\n", ui.Gray, ui.Reset)
		fmt.Printf("  %s• Pipe input: echo \"explain this\" | g-rump-cli%s\n", ui.Gray, ui.Reset)
		fmt.Println()

	default:
		fmt.Printf("  %sUnknown command: %s (type /help)%s\n\n", ui.Yellow, command, ui.Reset)
	}

	return false
}

func findSession(query string) *session.Session {
	// Try exact ID match first
	if sess, err := session.Load(query); err == nil {
		return sess
	}

	// Search by name
	infos := session.List()
	query = strings.ToLower(query)
	for _, info := range infos {
		if strings.ToLower(info.Name) == query || strings.HasPrefix(info.ID, query) {
			sess, err := session.Load(info.ID)
			if err == nil {
				return sess
			}
		}
	}

	// Fuzzy: check if name contains query
	for _, info := range infos {
		if strings.Contains(strings.ToLower(info.Name), query) {
			sess, err := session.Load(info.ID)
			if err == nil {
				return sess
			}
		}
	}

	return nil
}

func formatBytes(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%d B", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	}
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}
