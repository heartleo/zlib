package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/heartleo/zlib/internal/weread"
)

// WeReadPath is where WeRead credentials are saved.
func WeReadPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zlib", "weread.json")
}

// LoadWeRead returns the saved WeRead credentials. A missing file reports
// weread.ErrNotLoggedIn.
func LoadWeRead() (weread.Credentials, error) {
	data, err := os.ReadFile(WeReadPath())
	if errors.Is(err, os.ErrNotExist) {
		return weread.Credentials{}, weread.ErrNotLoggedIn
	}
	if err != nil {
		return weread.Credentials{}, err
	}
	var creds weread.Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return weread.Credentials{}, err
	}
	if !creds.Valid() {
		return weread.Credentials{}, weread.ErrNotLoggedIn
	}
	return creds, nil
}

// SaveWeRead writes creds through a temporary file and a rename, so a
// concurrent reader (such as another MCP call) never sees a partial file.
func SaveWeRead(creds weread.Credentials) error {
	path := WeReadPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	// CreateTemp opens the file with mode 0600.
	tmp, err := os.CreateTemp(dir, ".weread-*.json")
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

// RemoveWeRead deletes the saved credentials and reports whether a file
// existed.
func RemoveWeRead() (bool, error) {
	err := os.Remove(WeReadPath())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
