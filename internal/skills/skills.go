package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Skill struct {
	Name    string
	Path    string
	Content string
}

// LoadAll scans common agent directories for SKILL.md files.
func LoadAll() []Skill {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	searchDirs := []string{
		filepath.Join(home, ".gemini", "skills"),
		filepath.Join(home, ".claude", "skills"),
		filepath.Join(home, ".config", "agents", "skills"),
	}

	var skills []Skill
	seen := make(map[string]bool)

	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if entry.IsDir() {
				// Only target walt or other skill folders
				name := entry.Name()
				skillPath := filepath.Join(dir, name, "SKILL.md")
				
				if _, err := os.Stat(skillPath); err == nil {
					if !seen[name] {
						seen[name] = true
						skills = append(skills, Skill{
							Name: name,
							Path: skillPath,
						})
					}
				}
			}
		}
	}

	return skills
}

// ReadSkill loads the raw markdown of a specific skill
func ReadSkill(name string) (string, error) {
	skills := LoadAll()
	for _, s := range skills {
		if strings.EqualFold(s.Name, name) || strings.EqualFold(strings.TrimPrefix(s.Name, "walt-"), name) {
			data, err := os.ReadFile(s.Path)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
	}
	return "", fmt.Errorf("skill '%s' not found", name)
}
