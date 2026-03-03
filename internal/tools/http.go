package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func execHttpRequest(args map[string]interface{}) (string, error) {
	method := strings.ToUpper(str(args, "method"))
	url := str(args, "url")
	headersJSON := str(args, "headers")
	body := str(args, "body")

	if method == "" {
		return "", fmt.Errorf("method is required")
	}
	if url == "" {
		return "", fmt.Errorf("url is required")
	}

	// Validate method
	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true,
	}
	if !validMethods[method] {
		return "", fmt.Errorf("unsupported method: %s (use GET, POST, PUT, PATCH, DELETE, HEAD)", method)
	}

	// Build request
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	// Parse and set headers
	if headersJSON != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(headersJSON), &headers); err != nil {
			return "", fmt.Errorf("parsing headers JSON: %w", err)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
	}

	// Execute with timeout (configurable via GRUMP_HTTP_TIMEOUT env var)
	httpTimeout := envTimeout("GRUMP_HTTP_TIMEOUT", 30)
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	bodyStr := string(respBody)
	if len(bodyStr) > 20000 {
		bodyStr = bodyStr[:20000] + "\n... (truncated at 20000 chars)"
	}

	// Build result
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Status: %d %s\n", resp.StatusCode, resp.Status))
	sb.WriteString(fmt.Sprintf("Content-Type: %s\n", resp.Header.Get("Content-Type")))
	sb.WriteString(fmt.Sprintf("Content-Length: %s\n", resp.Header.Get("Content-Length")))
	sb.WriteString("\n")
	sb.WriteString(bodyStr)

	return sb.String(), nil
}
