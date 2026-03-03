package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type Context struct {
	CWD         string
	IsGit       bool
	GitBranch   string
	GitRemote   string
	ProjectType string
	ProjectName string
	BuCliMd     string

	gitOnce sync.Once
	gitDone chan struct{} // closed when FillGitInfo completes
}

// Detect reads the working directory for project metadata (full, blocking).
func Detect() *Context {
	ctx := DetectFast()
	ctx.fillGit()
	return ctx
}

// DetectFast does file-only detection (no subprocesses). Call FillGitInfo()
// afterwards in a goroutine for the git metadata.
func DetectFast() *Context {
	ctx := &Context{
		gitDone: make(chan struct{}),
	}
	ctx.CWD, _ = os.Getwd()

	// Check for .git directory (no subprocess)
	if info, err := os.Stat(filepath.Join(ctx.CWD, ".git")); err == nil && info.IsDir() {
		ctx.IsGit = true
	}

	// Project type
	detectors := []struct {
		file     string
		projType string
	}{
		{"go.mod", "go"},
		{"Cargo.toml", "rust"},
		{"package.json", "node"},
		{"pyproject.toml", "python"},
		{"requirements.txt", "python"},
		{"Gemfile", "ruby"},
		{"pom.xml", "java"},
		{"build.gradle", "java"},
		{"CMakeLists.txt", "c/c++"},
		{"Makefile", "make"},
	}
	for _, d := range detectors {
		if _, err := os.Stat(filepath.Join(ctx.CWD, d.file)); err == nil {
			ctx.ProjectType = d.projType
			ctx.ProjectName = filepath.Base(ctx.CWD)
			break
		}
	}

	// Walk up for .g-rump-cli.md
	ctx.BuCliMd = findBuCliMd(ctx.CWD)

	return ctx
}

// FillGitInfo populates git branch/remote via subprocesses. Safe to call from
// a goroutine. Calling it multiple times is a no-op after the first.
func (c *Context) FillGitInfo() {
	c.fillGit()
}

func (c *Context) fillGit() {
	c.gitOnce.Do(func() {
		defer close(c.gitDone)

		if !c.IsGit {
			return
		}

		if b, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output(); err == nil {
			c.GitBranch = strings.TrimSpace(string(b))
		}
		if r, err := exec.Command("git", "config", "--get", "remote.origin.url").Output(); err == nil {
			c.GitRemote = strings.TrimSpace(string(r))
		}
	})
}

// WaitGit blocks until git info has been filled.
func (c *Context) WaitGit() {
	<-c.gitDone
}

func findBuCliMd(dir string) string {
	for {
		path := filepath.Join(dir, ".g-rump-cli.md")
		if data, err := os.ReadFile(path); err == nil {
			content := string(data)
			if len(content) > 4000 {
				content = content[:4000] + "\n... (truncated)"
			}
			return content
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// StatusLine returns a one-line summary for the banner.
func (c *Context) StatusLine() string {
	parts := []string{}
	if c.ProjectName != "" {
		label := c.ProjectName
		if c.ProjectType != "" {
			label += " (" + c.ProjectType + ")"
		}
		parts = append(parts, label)
	}
	if c.IsGit && c.GitBranch != "" {
		parts = append(parts, c.GitBranch)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " • ")
}

// ForPrompt returns context info to embed in the system prompt.
func (c *Context) ForPrompt() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Working directory: %s\n", c.CWD))

	if c.ProjectName != "" {
		sb.WriteString(fmt.Sprintf("Project: %s", c.ProjectName))
		if c.ProjectType != "" {
			sb.WriteString(fmt.Sprintf(" (type: %s)", c.ProjectType))
		}
		sb.WriteString("\n")
	}

	if c.IsGit {
		sb.WriteString(fmt.Sprintf("Git branch: %s\n", c.GitBranch))
	}

	if c.BuCliMd != "" {
		sb.WriteString("\n## Project Instructions (.g-rump-cli.md)\n")
		sb.WriteString(c.BuCliMd)
		sb.WriteString("\n")
	}

	return sb.String()
}
