package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// loadDotEnv loads .env from the working directory, then from the global
// config dir (~/.config/zlib). godotenv never overrides variables that are
// already set, so precedence is: real environment > working-dir .env >
// config-dir .env.
func loadDotEnv() error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to determine current directory for .env loading: %w", err)
	}
	if err := loadDotEnvFrom(wd); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		// No home dir (rare); skip the global .env rather than fail startup.
		return nil
	}
	return loadDotEnvFrom(filepath.Join(home, ".config", "zlib"))
}

func loadDotEnvFrom(dir string) error {
	path := filepath.Join(dir, ".env")
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to access .env: %w", err)
	}

	if err := godotenv.Load(path); err != nil {
		return fmt.Errorf("failed to load .env: %w", err)
	}
	return nil
}
