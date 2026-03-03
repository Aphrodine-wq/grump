package changes

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Change struct {
	Type       string
	Path       string
	OldContent string
	Timestamp  time.Time
}

type Tracker struct {
	changes []Change
}

func NewTracker() *Tracker {
	return &Tracker{}
}

// Record stores a snapshot of a file before it was modified.
// oldContent is the full file content before the change (empty string if file was new).
func (t *Tracker) Record(changeType, path, oldContent string) {
	t.changes = append(t.changes, Change{
		Type:       changeType,
		Path:       path,
		OldContent: oldContent,
		Timestamp:  time.Now(),
	})
}

// Undo reverts the most recent change and returns a description.
func (t *Tracker) Undo() (string, error) {
	if len(t.changes) == 0 {
		return "", fmt.Errorf("nothing to undo")
	}

	last := t.changes[len(t.changes)-1]
	t.changes = t.changes[:len(t.changes)-1]

	if last.OldContent == "" {
		// File was newly created — remove it
		if err := os.Remove(last.Path); err != nil {
			return "", fmt.Errorf("removing %s: %w", last.Path, err)
		}
		return fmt.Sprintf("Removed %s (was newly created)", last.Path), nil
	}

	if err := os.WriteFile(last.Path, []byte(last.OldContent), 0644); err != nil {
		return "", fmt.Errorf("restoring %s: %w", last.Path, err)
	}
	return fmt.Sprintf("Restored %s to previous state", last.Path), nil
}

// Summary returns a human-readable summary of all tracked changes.
func (t *Tracker) Summary() string {
	if len(t.changes) == 0 {
		return "No file changes this session."
	}

	files := make(map[string]int)
	for _, c := range t.changes {
		files[c.Path]++
	}

	var lines []string
	for path, count := range files {
		s := ""
		if count > 1 {
			s = "s"
		}
		lines = append(lines, fmt.Sprintf("  %s (%d change%s)", path, count, s))
	}

	return fmt.Sprintf("%d change(s) across %d file(s):\n%s",
		len(t.changes), len(files), strings.Join(lines, "\n"))
}

func (t *Tracker) Count() int {
	return len(t.changes)
}
