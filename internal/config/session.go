package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Session struct {
	Cookies map[string]string `json:"cookies"`
	Domain  string            `json:"domain"`
}

func SessionPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zlib", "session.json")
}

func LoadSession() (Session, error) {
	data, err := os.ReadFile(SessionPath())
	if err != nil {
		return Session{}, err
	}
	var s Session
	return s, json.Unmarshal(data, &s)
}

func SaveSession(s Session) error {
	path := SessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
