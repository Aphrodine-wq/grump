package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"g-rump-cli/internal/ollama"
	"g-rump-cli/internal/ui"
)

// runWalkthrough launches the first-run setup wizard.
// Returns the populated Config to be saved.
func runWalkthrough(host string) Config {
	newCfg := Config{
		Host:  host,
		Model: "glm-5:cloud",
		Theme: "dark",
	}

	fmt.Println()
	r := ui.Reset
	cRed := "\033[38;5;196m"
	cOrange := "\033[38;5;202m"
	cYellow := "\033[38;5;226m"
	typeWrite(fmt.Sprintf("  %s      ██████████      %s", cRed, r))
	typeWrite(fmt.Sprintf("  %s    ██          ██    %s", cRed, r))
	typeWrite(fmt.Sprintf("  %s  ██  ████  ████  ██  %s", cOrange, r))
	typeWrite(fmt.Sprintf("  %s  ██              ██  %s", cOrange, r))
	typeWrite(fmt.Sprintf("  %s  ██    ██████    ██  %s", cYellow, r))
	typeWrite(fmt.Sprintf("  %s  ██  ██      ██  ██  %s", cYellow, r))
	typeWrite(fmt.Sprintf("  %s    ██          ██    %s", cRed, r))
	typeWrite(fmt.Sprintf("  %s      ██████████      %s", cRed, r))
	fmt.Println()
	typeWrite(fmt.Sprintf("  %sWelcome to the absolute apex of coding agents.%s Let's get you set up.\n", ui.Cyan+ui.Bold, ui.Reset))

	// ─── Model Picker ───
	fmt.Printf("  %s%s1. Choose your model%s\n\n", ui.Bold, ui.White, ui.Reset)

	// Fetch local Ollama models
	var localModels []string
	client := ollama.NewClient(newCfg.Host, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	models, err := client.ListModels(ctx)
	cancel()
	if err == nil {
		for _, m := range models {
			localModels = append(localModels, m.Name)
		}
	}

	// Merge: local first, then cloud (dedup)
	seen := map[string]bool{}
	var allModels []string
	for _, m := range localModels {
		seen[m] = true
		allModels = append(allModels, m)
	}
	for _, m := range KnownCloudModels {
		if !seen[m] {
			allModels = append(allModels, m)
		}
	}

	if len(allModels) == 0 {
		allModels = KnownCloudModels
	}

	// Find default index (glm-5:cloud)
	defaultIdx := 0
	for i, m := range allModels {
		if m == "glm-5:cloud" {
			defaultIdx = i
			break
		}
	}

	// Annotate local vs cloud
	var options []string
	for _, m := range allModels {
		label := m
		if contains(localModels, m) {
			label += fmt.Sprintf(" %s(local)%s", ui.Gray, ui.Reset)
		} else {
			label += fmt.Sprintf(" %s(cloud)%s", ui.Gray, ui.Reset)
		}
		options = append(options, label)
	}

	idx := ui.SelectOption(options, defaultIdx)
	newCfg.Model = allModels[idx]
	fmt.Printf("  %s✓ Model: %s%s\n\n", ui.Green, newCfg.Model, ui.Reset)

	// ─── Theme Picker ───
	fmt.Printf("  %s%s2. Choose your theme%s\n\n", ui.Bold, ui.White, ui.Reset)

	themeOptions := []string{
		fmt.Sprintf("Dark Mode  %s(purple/cyan on dark backgrounds)%s", ui.Gray, ui.Reset),
		fmt.Sprintf("Light Mode %s(deeper colors for white terminals)%s", ui.Gray, ui.Reset),
	}
	themeIdx := ui.SelectOption(themeOptions, 0)
	if themeIdx == 1 {
		newCfg.Theme = "light"
		ui.SetTheme("light")
	} else {
		newCfg.Theme = "dark"
		ui.SetTheme("dark")
	}
	fmt.Printf("  %s✓ Theme: %s%s\n\n", ui.Green, newCfg.Theme, ui.Reset)

	// ─── Host Confirmation ───
	fmt.Printf("  %s%s3. Ollama host%s\n\n", ui.Bold, ui.White, ui.Reset)
	fmt.Printf("  %sCurrent: %s%s%s\n", ui.Gray, ui.Cyan, newCfg.Host, ui.Reset)

	hostOptions := []string{
		fmt.Sprintf("Keep %s", newCfg.Host),
		"Change host",
	}
	hostIdx := ui.SelectOption(hostOptions, 0)
	if hostIdx == 1 {
		fmt.Printf("  %sEnter Ollama URL:%s ", ui.Gray, ui.Reset)
		var newHost string
		fmt.Scanln(&newHost)
		newHost = strings.TrimSpace(newHost)
		if newHost != "" {
			newCfg.Host = newHost
		}
	}
	fmt.Printf("  %s✓ Host: %s%s\n\n", ui.Green, newCfg.Host, ui.Reset)

	// ─── Desktop Folder ───
	fmt.Printf("  %s%s4. Desktop shortcut%s\n\n", ui.Bold, ui.White, ui.Reset)

	deskOptions := []string{
		fmt.Sprintf("Create ~/Desktop/G-Rump-CLI/ %s(symlink + quick-start guide)%s", ui.Gray, ui.Reset),
		"Skip",
	}
	deskIdx := ui.SelectOption(deskOptions, 0)
	if deskIdx == 0 {
		if err := createDesktopFolder(); err != nil {
			fmt.Printf("  %s[-] %v%s\n\n", ui.Red, err, ui.Reset)
		} else {
			fmt.Printf("  %s[+] Created ~/Desktop/G-Rump-CLI/%s\n\n", ui.Green, ui.Reset)
		}
	} else {
		fmt.Printf("  %sSkipped. You can always run /install later.%s\n\n", ui.Gray, ui.Reset)
	}

	// ─── Cloud models run on Ollama natively (local or :cloud tag) ───
	if strings.Contains(newCfg.Model, "cloud") || strings.Contains(newCfg.Model, ":") {
		fmt.Printf("  %s%s5. Ollama Cloud Model%s\n\n", ui.Bold, ui.White, ui.Reset)
		fmt.Printf("  %sYou selected a cloud-capable model. Make sure Ollama is running:%s\n", ui.Cyan, ui.Reset)
		fmt.Printf("  %s  ollama serve%s\n", ui.Gray, ui.Reset)
		fmt.Printf("  %sThen pull the model:%s\n", ui.Cyan, ui.Reset)
		fmt.Printf("  %s  ollama pull %s%s\n\n", ui.Gray, newCfg.Model, ui.Reset)
		fmt.Printf("  %sOllama handles cloud execution natively via the :cloud tag.%s\n\n", ui.Gray, ui.Reset)
	}

	// ─── Done ───
	fmt.Printf("  %s%sSetup complete!%s Saving config to ~/.g-rump-cli/config.json\n\n", ui.Bold, ui.Green, ui.Reset)

	return newCfg
}

// createDesktopFolder creates ~/Desktop/G-Rump-CLI/ with a symlink and README.
func createDesktopFolder() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	dir := filepath.Join(home, "Desktop", "G-Rump-CLI")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	// Symlink to the binary
	binPath, err := os.Executable()
	if err == nil {
		linkPath := filepath.Join(dir, "g-rump-cli")
		os.Remove(linkPath) // remove existing to update
		os.Symlink(binPath, linkPath)
	}

	// README
	readme := `# G-Rump-CLI — AI Coding Assistant

## Quick Start

    g-rump-cli              Start the interactive REPL
    g-rump-cli models       List available models
    echo "task" | g-rump-cli    Pipe a task directly

## Commands

    /help       Show all commands
    /model      Show or switch model
    /theme      Switch between dark/light mode
    /clear      Clear conversation
    /compact    Trim old messages
    /session    Save current session
    /sessions   List saved sessions
    /resume     Resume a saved session
    /undo       Undo the last file change
    /changes    Show file changes
    /cd         Change working directory
    /save       Save settings
    /install    Recreate this desktop folder
    /exit       Exit G-Rump-CLI

## Config

Settings are stored in ~/.g-rump-cli/config.json

## Tips

- Start with """ for multi-line input, end with """
- End a line with \ to continue on the next line
- The AI can read, write, edit files and run shell commands
- Use /undo to revert the most recent file change
`
	readmePath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("writing README: %w", err)
	}

	return nil
}

// typeWrite prints a line quickly.
func typeWrite(line string) {
	fmt.Println(line)
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
