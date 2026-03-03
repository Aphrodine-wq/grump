package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Types ---

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// ... Keep existing types ...
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	Id       string           `json:"id,omitempty"` // Added for OpenAI compatibility
	Type     string           `json:"type,omitempty"` // Added for OpenAI compatibility
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (f ToolCallFunction) ParseArgs() (map[string]interface{}, error) {
	var args map[string]interface{}
	if err := json.Unmarshal(f.Arguments, &args); err != nil {
		var str string
		if err2 := json.Unmarshal(f.Arguments, &str); err2 == nil {
			if err3 := json.Unmarshal([]byte(str), &args); err3 == nil {
				return args, nil
			}
		}
		return nil, fmt.Errorf("cannot parse arguments: %w", err)
	}
	return args, nil
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  ToolParams `json:"parameters"`
}

type ToolParams struct {
	Type       string                  `json:"type"`
	Properties map[string]ToolProperty `json:"properties"`
	Required   []string                `json:"required"`
}

type ToolProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ChatRequest struct {
	Model    string          `json:"model"`
	Messages []Message       `json:"messages"`
	Tools    json.RawMessage `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
}

type ChatResponse struct {
	Model         string  `json:"model"`
	Message       Message `json:"message"`
	Done          bool    `json:"done"`
	TotalDuration int64   `json:"total_duration,omitempty"`
	EvalCount     int     `json:"eval_count,omitempty"`
	EvalDuration  int64   `json:"eval_duration,omitempty"`
}

// OpenAI specific structures
type OpenAIChatResponse struct {
	Choices []struct {
		Delta struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls,omitempty"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type StreamResult struct {
	Content   string
	ToolCalls []ToolCall
	Duration  time.Duration
	EvalCount int
	EvalDur   int64
}

type ModelInfo struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	Size       int64  `json:"size"`
	ModifiedAt string `json:"modified_at"`
}

type ModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

// scannerPool reuses bufio.Scanner buffers to reduce GC pressure.
var scannerPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 0, 64*1024)
		return buf
	},
}

// --- Client ---

func NewClient(baseURL string, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: (&net.Dialer{
					Timeout:   2 * time.Second,
					KeepAlive: 300 * time.Second,
				}).DialContext,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   100,
				MaxConnsPerHost:       100,
				IdleConnTimeout:       300 * time.Second,
				DisableCompression:    true,
				ForceAttemptHTTP2:     true,
				ExpectContinueTimeout: 1 * time.Second,
				WriteBufferSize:       128 * 1024,
				ReadBufferSize:        128 * 1024,
			},
		},
	}
}

// Host returns the base URL of this client.
func (c *Client) Host() string {
	return c.baseURL
}

// isOpenAICompatible returns true if the endpoint uses OpenAI-compatible format.
// Only triggered by URL pattern (contains "/v1"), NOT by API key alone.
// This prevents Ollama-native endpoints from being misrouted when an API key
// is set for authentication (e.g., reverse proxies, Ollama with auth).
func (c *Client) isOpenAICompatible() bool {
	return strings.Contains(c.baseURL, "/v1")
}

// Ping sends a lightweight request to warm up the connection pool.
func (c *Client) Ping(ctx context.Context) error {
	if c.isOpenAICompatible() {
		return nil // Skip ping for OpenAI endpoints
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/tags", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) ChatStream(ctx context.Context, req ChatRequest, onToken func(string)) (*StreamResult, error) {
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	maxRetries := 1
	if v := os.Getenv("GRUMP_API_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			maxRetries = n
		}
	}
	var lastErr error

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry (exponential backoff)
			delay := time.Duration(attempt) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		var result *StreamResult
		var reqErr error

		if c.isOpenAICompatible() {
			result, reqErr = c.doStreamOpenAI(ctx, body, onToken)
		} else {
			result, reqErr = c.doStream(ctx, body, onToken)
		}

		if reqErr == nil {
			return result, nil
		}
		lastErr = reqErr

		// Only retry if we haven't sent any tokens yet (connection-level failures)
		if result != nil && result.Content != "" {
			return nil, reqErr
		}
	}

	return nil, classifyStreamError(lastErr, maxRetries)
}

func (c *Client) doStreamOpenAI(ctx context.Context, body []byte, onToken func(string)) (*StreamResult, error) {
	url := c.baseURL
	if !strings.HasSuffix(url, "/chat/completions") {
		url = strings.TrimSuffix(url, "/") + "/chat/completions"
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
		httpReq.Header.Set("HTTP-Referer", "https://g-rump-cli.local")
		httpReq.Header.Set("X-Title", "G-Rump-CLI")
	}

	start := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("connecting to API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("api error %d: %s", resp.StatusCode, string(errBody))
	}

	poolBuf := scannerPool.Get().([]byte)
	defer scannerPool.Put(poolBuf[:0])

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(poolBuf, 1024*1024)

	var result StreamResult
	var content strings.Builder
	var toolCallsMap = make(map[int]*ToolCall)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk OpenAIChatResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta
			if delta.Content != "" {
				onToken(delta.Content)
				content.WriteString(delta.Content)
			}

			// Aggregate tool calls (OpenAI streams tool arguments in chunks)
			for i, tc := range delta.ToolCalls {
				if existing, ok := toolCallsMap[i]; ok {
					if tc.Function.Arguments != nil {
						args := string(existing.Function.Arguments)
						args = strings.TrimSuffix(args, "\"")
						if strings.HasPrefix(args, "\"") { args = args[1:] }
						
						newArgs := string(tc.Function.Arguments)
						newArgs = strings.TrimPrefix(strings.TrimSuffix(newArgs, "\""), "\"")
						
						existing.Function.Arguments = json.RawMessage(args + newArgs)
					}
				} else {
					// Copy to avoid referencing loop variable
					newTc := tc
					// If the arguments are empty, initialize as empty JSON string if not present
					if len(newTc.Function.Arguments) == 0 {
						newTc.Function.Arguments = json.RawMessage(`""`)
					}
					toolCallsMap[i] = &newTc
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		result.Content = content.String()
		return &result, fmt.Errorf("reading stream: %w", err)
	}

	result.Content = content.String()
	for i := 0; i < len(toolCallsMap); i++ {
		tc := toolCallsMap[i]
		
		// Clean up the arguments string
		argStr := string(tc.Function.Arguments)
		if strings.HasPrefix(argStr, "\"") && strings.HasSuffix(argStr, "\"") {
			var unescaped string
			json.Unmarshal(tc.Function.Arguments, &unescaped)
			tc.Function.Arguments = json.RawMessage(unescaped)
		}
		if len(tc.Function.Arguments) == 0 {
			tc.Function.Arguments = json.RawMessage(`{}`)
		}

		result.ToolCalls = append(result.ToolCalls, *tc)
	}
	result.Duration = time.Since(start)

	return &result, nil
}

func (c *Client) doStream(ctx context.Context, body []byte, onToken func(string)) (*StreamResult, error) {
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("connecting to Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ollama error %d: %s", resp.StatusCode, string(errBody))
	}

	// Get a buffer from the pool
	poolBuf := scannerPool.Get().([]byte)
	defer scannerPool.Put(poolBuf[:0])

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(poolBuf, 1024*1024)

	var result StreamResult
	var content strings.Builder

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var chunk ChatResponse
		if err := json.Unmarshal(line, &chunk); err != nil {
			continue
		}

		if chunk.Message.Content != "" {
			onToken(chunk.Message.Content)
			content.WriteString(chunk.Message.Content)
		}

		if len(chunk.Message.ToolCalls) > 0 {
			result.ToolCalls = append(result.ToolCalls, chunk.Message.ToolCalls...)
		}

		if chunk.Done {
			result.Duration = time.Duration(chunk.TotalDuration)
			result.EvalCount = chunk.EvalCount
			result.EvalDur = chunk.EvalDuration
		}
	}

	if err := scanner.Err(); err != nil {
		result.Content = content.String()
		return &result, fmt.Errorf("reading stream: %w", err)
	}

	result.Content = content.String()
	return &result, nil
}

func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var result ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return result.Models, nil
}

// ClassifyError returns a user-friendly error message with actionable suggestions.
func ClassifyError(err error, host string, model string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	switch {
	case strings.Contains(msg, "connection refused"):
		return fmt.Sprintf("Cannot connect to Ollama at %s.\n  Is Ollama running? Start it with: ollama serve\n  Or set a different host with: /host <url>", host)
	case strings.Contains(msg, "no such host") || strings.Contains(msg, "dial tcp"):
		return fmt.Sprintf("Cannot reach host %s.\n  Check your network connection and host URL.\n  Update with: /host <url>", host)
	case strings.Contains(msg, "404") || strings.Contains(msg, "not found"):
		return fmt.Sprintf("Model '%s' not found.\n  Pull it with: ollama pull %s\n  Or switch models with: /model <name>", model, model)
	case strings.Contains(msg, "401") || strings.Contains(msg, "403") || strings.Contains(msg, "Unauthorized") || strings.Contains(msg, "authentication"):
		return "Authentication failed.\n  Check your API key with: /key <your-key>\n  Or verify your API host with: /host"
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return "Request timed out.\n  The model may still be loading. Try again in a moment.\n  For large models, the first request can take 30-60 seconds."
	case strings.Contains(msg, "429") || strings.Contains(msg, "rate limit"):
		return "Rate limited by the API.\n  Wait a moment and try again.\n  Consider switching to a local model with: /model <local-model>"
	case strings.Contains(msg, "500") || strings.Contains(msg, "502") || strings.Contains(msg, "503"):
		return fmt.Sprintf("Server error from %s.\n  The service may be temporarily unavailable. Try again shortly.", host)
	default:
		return fmt.Sprintf("Error: %v", err)
	}
}

// classifyStreamError wraps stream errors with actionable hints.
func classifyStreamError(err error, retries int) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	switch {
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("connection refused. Is Ollama running? Start with: ollama serve")
	case strings.Contains(msg, "loading") || strings.Contains(msg, "initializing"):
		return fmt.Errorf("model may be loading. Try /retry in 30 seconds (%w)", err)
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return fmt.Errorf("request timed out. Model may be loading — try /retry in 30s (%w)", err)
	default:
		return fmt.Errorf("after %d retries: %w", retries, err)
	}
}

// PreSerializeTools serializes a tool list once, returning json.RawMessage
// suitable for ChatRequest.Tools. Avoids re-serializing on every API call.
func PreSerializeTools(tools []Tool) json.RawMessage {
	data, err := json.Marshal(tools)
	if err != nil {
		return nil
	}
	return json.RawMessage(data)
}
