package logging

import (
	"fmt"
	"os"
	"time"
)

// Debug controls whether debug output is printed.
var Debug bool

// Debugf prints a timestamped debug message to stderr when Debug is true.
func Debugf(format string, args ...interface{}) {
	if !Debug {
		return
	}
	ts := time.Now().Format("15:04:05")
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "\033[2m[DEBUG %s] %s\033[0m\n", ts, msg)
}
