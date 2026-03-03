package ui

// Theme holds all color escape codes for a visual theme.
type Theme struct {
	Name   string
	Purple string
	Cyan   string
	Red    string
	Yellow string
	Green  string
	Gray   string
	White  string
}

// DarkTheme is the default — purple/cyan on dark backgrounds.
var DarkTheme = Theme{
	Name:   "dark",
	Purple: "\033[38;5;141m",
	Cyan:   "\033[38;5;117m",
	Red:    "\033[38;5;203m",
	Yellow: "\033[38;5;221m",
	Green:  "\033[38;5;114m",
	Gray:   "\033[38;5;245m",
	White:  "\033[38;5;255m",
}

// LightTheme uses deeper/darker colors readable on white backgrounds.
var LightTheme = Theme{
	Name:   "light",
	Purple: "\033[38;5;91m",
	Cyan:   "\033[38;5;30m",
	Red:    "\033[38;5;160m",
	Yellow: "\033[38;5;130m",
	Green:  "\033[38;5;28m",
	Gray:   "\033[38;5;240m",
	White:  "\033[38;5;16m",
}

// Themes maps theme names to their definitions.
var Themes = map[string]Theme{
	"dark":  DarkTheme,
	"light": LightTheme,
}

// CurrentTheme tracks the active theme name.
var CurrentTheme = "dark"

// SetTheme applies the named theme by updating the package-level color variables.
func SetTheme(name string) bool {
	t, ok := Themes[name]
	if !ok {
		return false
	}
	CurrentTheme = name
	Purple = t.Purple
	Cyan = t.Cyan
	Red = t.Red
	Yellow = t.Yellow
	Green = t.Green
	Gray = t.Gray
	White = t.White
	return true
}
