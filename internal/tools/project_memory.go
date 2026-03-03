package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ProjectMemoryStore struct {
	Architecture []string `json:"architecture"`
	Dependencies []string `json:"dependencies"`
	Rules        []string `json:"rules"`
	Custom       []string `json:"custom"`
}

func getProjectMemoryPath() string {
	cwd, _ := os.Getwd()
	return filepath.Join(cwd, ".bu", "knowledge.json")
}

func loadProjectMemory() (*ProjectMemoryStore, error) {
	path := getProjectMemoryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectMemoryStore{}, nil
		}
		return nil, err
	}

	var store ProjectMemoryStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return &store, nil
}

func saveProjectMemory(store *ProjectMemoryStore) error {
	path := getProjectMemoryPath()
	
	// Create .bu directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func ToolRememberProjectFact(args map[string]interface{}) (string, error) {
	category := str(args, "category") // architecture, dependencies, rules, custom
	fact := str(args, "fact")

	if category == "" || fact == "" {
		return "", fmt.Errorf("both category and fact are required")
	}

	store, err := loadProjectMemory()
	if err != nil {
		return "", fmt.Errorf("could not load project memory: %v", err)
	}

	switch strings.ToLower(category) {
	case "architecture":
		store.Architecture = append(store.Architecture, fact)
	case "dependencies":
		store.Dependencies = append(store.Dependencies, fact)
	case "rules":
		store.Rules = append(store.Rules, fact)
	default:
		store.Custom = append(store.Custom, fact)
	}

	if err := saveProjectMemory(store); err != nil {
		return "", fmt.Errorf("could not save project memory: %v", err)
	}

	return fmt.Sprintf("Successfully added to .bu project knowledge base [%s]: %s", category, fact), nil
}

func GetProjectMemoryContext() string {
	store, err := loadProjectMemory()
	if err != nil {
		return ""
	}

	hasData := len(store.Architecture) > 0 || len(store.Dependencies) > 0 || len(store.Rules) > 0 || len(store.Custom) > 0
	if !hasData {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Local Project Knowledge Base (.bu/knowledge.json)\n")
	sb.WriteString("The following facts are specific to THIS specific codebase and must be strictly adhered to:\n")

	if len(store.Architecture) > 0 {
		sb.WriteString("\n### Architecture:\n")
		for _, fact := range store.Architecture {
			sb.WriteString(fmt.Sprintf("- %s\n", fact))
		}
	}
	if len(store.Dependencies) > 0 {
		sb.WriteString("\n### Dependencies & Libraries:\n")
		for _, fact := range store.Dependencies {
			sb.WriteString(fmt.Sprintf("- %s\n", fact))
		}
	}
	if len(store.Rules) > 0 {
		sb.WriteString("\n### Codebase Rules:\n")
		for _, fact := range store.Rules {
			sb.WriteString(fmt.Sprintf("- %s\n", fact))
		}
	}
	if len(store.Custom) > 0 {
		sb.WriteString("\n### General Knowledge:\n")
		for _, fact := range store.Custom {
			sb.WriteString(fmt.Sprintf("- %s\n", fact))
		}
	}
	sb.WriteString("\n")
	return sb.String()
}