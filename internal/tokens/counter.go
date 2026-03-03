package tokens

import (
	"encoding/json"
	"fmt"
	"strings"

	"g-rump-cli/internal/ollama"
)

// EstimateTokens estimates the token count for a string using a simple heuristic.
// Most LLM tokenizers average ~4 characters per token for English text.
func EstimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return len(text) / 4
}

// EstimateMessages sums token estimates across all messages including role
// overhead, content, and tool call arguments.
func EstimateMessages(msgs []ollama.Message) int {
	total := 0
	for _, m := range msgs {
		// Role name overhead (~4 tokens for role framing)
		total += 4
		total += EstimateTokens(m.Content)
		for _, tc := range m.ToolCalls {
			total += EstimateTokens(tc.Function.Name)
			if len(tc.Function.Arguments) > 0 {
				// Estimate from the raw JSON
				argStr, _ := json.Marshal(tc.Function.Arguments)
				total += EstimateTokens(string(argStr))
			}
		}
	}
	return total
}

// modelCosts maps known model prefixes to approximate cost per 1M tokens (input, output) in USD.
var modelCosts = map[string][2]float64{
	"deepseek":  {0.14, 2.19},
	"qwen":      {0.30, 1.20},
	"llama":     {0.05, 0.10},
	"glm":       {0.10, 0.10},
	"mistral":   {0.25, 0.25},
	"gemma":     {0.07, 0.07},
	"phi":       {0.07, 0.07},
	"command-r": {0.50, 1.50},
	"claude":    {3.00, 15.00},
	"gpt-4":     {10.00, 30.00},
	"gpt-3.5":   {0.50, 1.50},
}

// EstimateCost returns estimated cost in USD for the given model and token counts.
// The second return value indicates whether the model was found in the cost map.
func EstimateCost(model string, inputTokens int, outputTokens int) (float64, bool) {
	lowerModel := strings.ToLower(model)
	for prefix, costs := range modelCosts {
		if strings.Contains(lowerModel, prefix) {
			inputCost := float64(inputTokens) / 1_000_000 * costs[0]
			outputCost := float64(outputTokens) / 1_000_000 * costs[1]
			return inputCost + outputCost, true
		}
	}
	return 0, false
}

// FormatTokenCount returns a human-readable token count string.
func FormatTokenCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
