package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

func loadDotEnv() error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to determine current directory for .env loading: %w", err)
	}
	return loadDotEnvFrom(wd)
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
