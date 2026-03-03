package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"g-rump-cli/internal/ollama"
	"g-rump-cli/internal/ui"
)

// KnownCloudModels are popular models available on Ollama (cloud or local).
var KnownCloudModels = []string{
	"glm-5:cloud",
	"minimax-m2.5:cloud",
	"kimi-k2.5:cloud",
	"deepseek-r1:cloud",
	"deepseek-coder-v2:cloud",
	"qwen2.5-coder:32b",
	"qwen3:cloud",
	"gemma3:cloud",
	"phi4:cloud",
	"llama3.3",
	"mistral-large:cloud",
	"command-r-plus:cloud",
}

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List available Ollama models",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := ollama.NewClient(cfg.Host, cfg.APIKey)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		models, err := client.ListModels(ctx)
		if err != nil {
			return fmt.Errorf("cannot connect to Ollama: %w", err)
		}

		// --- Local Models ---
		fmt.Printf("\n%s%sLocal Models%s\n", ui.Bold, ui.White, ui.Reset)
		fmt.Printf("%s────────────────────────────────────────%s\n", ui.Gray, ui.Reset)

		// Build set of local model names for dedup
		localSet := map[string]bool{}

		if len(models) == 0 {
			fmt.Printf("  %s(none — pull one with: ollama pull glm-5:cloud)%s\n", ui.Gray, ui.Reset)
		} else {
			for _, m := range models {
				localSet[m.Name] = true
				active := " "
				if m.Name == cfg.Model {
					active = fmt.Sprintf("%s●%s", ui.Green, ui.Reset)
				}

				size := ""
				if m.Size > 1024*1024 {
					gb := float64(m.Size) / (1024 * 1024 * 1024)
					size = fmt.Sprintf("%.1f GB", gb)
				} else {
					size = "cloud"
				}

				fmt.Printf(" %s  %s%-28s%s  %s%s%s\n",
					active,
					ui.Cyan, m.Name, ui.Reset,
					ui.Gray, size, ui.Reset)
			}
		}

		// --- Cloud Models ---
		fmt.Printf("\n%s%sCloud Models%s %s(ollama pull <name>)%s\n", ui.Bold, ui.White, ui.Reset, ui.Gray, ui.Reset)
		fmt.Printf("%s────────────────────────────────────────%s\n", ui.Gray, ui.Reset)

		for _, name := range KnownCloudModels {
			if localSet[name] {
				continue // already shown above
			}
			active := " "
			if name == cfg.Model {
				active = fmt.Sprintf("%s●%s", ui.Green, ui.Reset)
			}
			fmt.Printf(" %s  %s%-28s%s  %scloud%s\n",
				active,
				ui.Purple, name, ui.Reset,
				ui.Gray, ui.Reset)
		}
		fmt.Println()

		return nil
	},
}

func init() {
	rootCmd.AddCommand(modelsCmd)
}
