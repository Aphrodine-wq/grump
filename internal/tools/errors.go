package tools

import "strings"

// WrapToolError enhances raw Go errors with actionable user-facing messages.
func WrapToolError(toolName string, err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()

	switch {
	case strings.Contains(msg, "context deadline exceeded"):
		switch toolName {
		case "bash":
			return wrapf(err, "Command timed out. Increase with GRUMP_BASH_TIMEOUT env var (default: 120s)")
		case "run_tests":
			return wrapf(err, "Tests timed out. Increase with GRUMP_TEST_TIMEOUT env var (default: 300s)")
		case "http_request":
			return wrapf(err, "HTTP request timed out. Increase with GRUMP_HTTP_TIMEOUT env var (default: 30s)")
		case "fetch_webpage", "search_web":
			return wrapf(err, "Fetch timed out. Increase with GRUMP_FETCH_TIMEOUT env var (default: 5s)")
		default:
			return wrapf(err, "Operation timed out")
		}

	case strings.Contains(msg, "permission denied"):
		return wrapf(err, "Permission denied. Check file permissions with: ls -la <path>")

	case strings.Contains(msg, "no such file or directory"):
		return wrapf(err, "File not found. Check the path and try again")

	case strings.Contains(msg, "connection refused"):
		return wrapf(err, "Connection refused. Is the server running?")

	case strings.Contains(msg, "certificate") || strings.Contains(msg, "x509"):
		return wrapf(err, "SSL/TLS certificate error. Check the URL or try with http:// instead of https://")

	case strings.Contains(msg, "too many open files"):
		return wrapf(err, "Too many open files. Close some processes or increase ulimit: ulimit -n 4096")

	case strings.Contains(msg, "address already in use"):
		return wrapf(err, "Port already in use. Find the process with: lsof -i :<port>")
	}

	return err
}

type wrappedError struct {
	hint string
	err  error
}

func (w *wrappedError) Error() string {
	return w.hint + " (" + w.err.Error() + ")"
}

func (w *wrappedError) Unwrap() error {
	return w.err
}

func wrapf(err error, hint string) error {
	return &wrappedError{hint: hint, err: err}
}
