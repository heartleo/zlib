package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/heartleo/zlib/internal/aimatch"
)

var aiMu sync.Mutex

// AIPath is where the AI model settings, including the API key, are saved.
func AIPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zlib", "ai.json")
}

// LoadAI returns the saved AI settings; a missing file is the zero value.
func LoadAI() (aimatch.Settings, error) {
	data, err := os.ReadFile(AIPath())
	if errors.Is(err, os.ErrNotExist) {
		return aimatch.Settings{}, nil
	}
	if err != nil {
		return aimatch.Settings{}, err
	}
	var s aimatch.Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return aimatch.Settings{}, err
	}
	return s, nil
}

// SaveAI writes s through a temporary file and a rename at mode 0600.
func SaveAI(s aimatch.Settings) error {
	aiMu.Lock()
	defer aiMu.Unlock()
	path := AIPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".ai-*.json") // mode 0600
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// RemoveAI deletes the saved settings and reports whether a file existed.
func RemoveAI() (bool, error) {
	aiMu.Lock()
	defer aiMu.Unlock()
	err := os.Remove(AIPath())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
