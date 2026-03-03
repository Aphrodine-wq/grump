package ui

import (
	"os"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

// RenderMarkdown renders markdown content as ANSI-formatted terminal output
// using the current theme (dark or light). Falls back to raw content on error.
func RenderMarkdown(content string) string {
	dark := CurrentTheme == "dark"
	return RenderMarkdownWithTheme(content, dark)
}

// RenderMarkdownWithTheme renders markdown with an explicit dark/light choice.
func RenderMarkdownWithTheme(content string, dark bool) string {
	style := "dark"
	if !dark {
		style = "light"
	}

	width := 100
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		width = w - 4
		if width < 40 {
			width = 40
		}
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content
	}

	rendered, err := r.Render(content)
	if err != nil {
		return content
	}

	return rendered
}
