package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MemoryStore struct {
	Facts []string `json:"facts"`
}

func getMemoryPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".g-rump-cli", "memory.json")
}

func loadMemory() (*MemoryStore, error) {
	path := getMemoryPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &MemoryStore{Facts: []string{}}, nil
		}
		return nil, err
	}

	var store MemoryStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return &store, nil
}

func saveMemory(store *MemoryStore) error {
	path := getMemoryPath()
	os.MkdirAll(filepath.Dir(path), 0755)
	
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func ToolSaveMemory(args map[string]interface{}) (string, error) {
	fact, ok := args["fact"].(string)
	if !ok || strings.TrimSpace(fact) == "" {
		return "", fmt.Errorf("fact is required")
	}

	store, err := loadMemory()
	if err != nil {
		return "", fmt.Errorf("could not load memory: %v", err)
	}

	// Avoid exact duplicates
	for _, existing := range store.Facts {
		if existing == fact {
			return "Fact already known.", nil
		}
	}

	store.Facts = append(store.Facts, fact)
	if err := saveMemory(store); err != nil {
		return "", fmt.Errorf("could not save memory: %v", err)
	}

	return fmt.Sprintf("Successfully remembered: %s", fact), nil
}

func GetGlobalMemoryContext() string {
	store, err := loadMemory()
	if err != nil || len(store.Facts) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Global User Memory (Persisted Across Sessions)\n")
	sb.WriteString("The following facts are preferences or architectural constraints the user wants you to ALWAYS remember:\n")
	for i, fact := range store.Facts {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, fact))
	}
	sb.WriteString("\n")
	return sb.String()
}