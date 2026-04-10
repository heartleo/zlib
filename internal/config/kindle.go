package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/heartleo/zlib"
)

func KindleConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "zlib", "kindle.json")
}

func LoadKindleConfig() (zlib.KindleConfig, error) {
	data, err := os.ReadFile(KindleConfigPath())
	if err != nil {
		return zlib.KindleConfig{}, err
	}
	var cfg zlib.KindleConfig
	return cfg, json.Unmarshal(data, &cfg)
}

func SaveKindleConfig(cfg zlib.KindleConfig) error {
	path := KindleConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
