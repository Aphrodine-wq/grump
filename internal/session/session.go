package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"g-rump-cli/internal/ollama"
)

type Session struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Model    string           `json:"model"`
	Messages []ollama.Message `json:"messages"`
	Created  time.Time        `json:"created"`
	Updated  time.Time        `json:"updated"`
}

type Info struct {
	ID       string
	Name     string
	Updated  time.Time
	Messages int
}

func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".g-rump-cli", "sessions")
}

func Save(s Session) error {
	dir := Dir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	s.Updated = time.Now()
	if s.Created.IsZero() {
		s.Created = s.Updated
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	finalPath := filepath.Join(dir, s.ID+".json")
	tmpPath := finalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath) // clean up on rename failure
		return err
	}
	return nil
}

func Load(id string) (*Session, error) {
	data, err := os.ReadFile(filepath.Join(Dir(), id+".json"))
	if err != nil {
		return nil, fmt.Errorf("session %q not found", id)
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func List() []Info {
	entries, err := os.ReadDir(Dir())
	if err != nil {
		return nil
	}

	var infos []Info
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(Dir(), e.Name()))
		if err != nil {
			continue
		}
		var s Session
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		msgs := 0
		for _, m := range s.Messages {
			if m.Role == "user" {
				msgs++
			}
		}
		infos = append(infos, Info{
			ID:       s.ID,
			Name:     s.Name,
			Updated:  s.Updated,
			Messages: msgs,
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Updated.After(infos[j].Updated)
	})
	return infos
}

func Delete(id string) error {
	return os.Remove(filepath.Join(Dir(), id+".json"))
}

// GarbageCollect deletes session files older than maxAgeDays.
// Returns the number of deleted sessions and total bytes freed.
func GarbageCollect(maxAgeDays int) (int, int64, error) {
	dir := Dir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, err
	}

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	deleted := 0
	var freed int64

	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			freed += info.Size()
			if err := os.Remove(path); err == nil {
				deleted++
			}
		}
	}

	return deleted, freed, nil
}
