package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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

// dotEnvDomainRe matches a ZLIB_DOMAIN assignment, with or without the `export`
// prefix godotenv also accepts. It anchors at the start of the line, so a
// commented-out assignment is left alone.
var dotEnvDomainRe = regexp.MustCompile(`^\s*(?:export\s+)?ZLIB_DOMAIN\s*=`)

// globalDotEnvPath returns the path of the config-dir .env.
func globalDotEnvPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "zlib", ".env"), nil
}

// persistDotEnvDomain records domain as ZLIB_DOMAIN in the config-dir .env so
// the commands that follow a login reach the mirror that login actually used.
//
// Without it, `zlib login --domain X` succeeds and every later command still
// resolves whatever ZLIB_DOMAIN happens to hold, which users reasonably forget
// to update. It reports the path and whether the file changed so the caller can
// say what it did.
//
// Note the precedence this cannot beat: a real environment variable and a
// working-directory .env both outrank the config-dir file, so a stale value in
// either still wins over what this writes.
func persistDotEnvDomain(domain string) (string, bool, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", false, nil
	}
	path, err := globalDotEnvPath()
	if err != nil {
		return "", false, err
	}

	original, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return path, false, fmt.Errorf("failed to read %s: %w", path, err)
	}

	updated := replaceDotEnvDomain(string(original), domain)
	if updated == string(original) {
		return path, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return path, false, fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
		return path, false, fmt.Errorf("failed to write %s: %w", path, err)
	}
	return path, true, nil
}

// replaceDotEnvDomain rewrites the first ZLIB_DOMAIN assignment in content and
// drops any later ones, appending the assignment when there is none. Every
// other line is preserved byte for byte: the same file holds ZLIB_SMTP_PWD and
// ZLIB_PROXY, and rewriting it wholesale would lose the user's comments and
// ordering.
func replaceDotEnvDomain(content, domain string) string {
	assignment := "ZLIB_DOMAIN=" + domain
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines)+1)
	replaced := false
	for _, line := range lines {
		if !dotEnvDomainRe.MatchString(line) {
			kept = append(kept, line)
			continue
		}
		if replaced {
			// A later duplicate would shadow the line just written.
			continue
		}
		kept = append(kept, assignment)
		replaced = true
	}
	if replaced {
		return strings.Join(kept, "\n")
	}

	// Appending: end with exactly one newline, whether or not the file had a
	// trailing one and whether or not it was empty.
	body := strings.TrimRight(content, "\n")
	if body == "" {
		return assignment + "\n"
	}
	return body + "\n" + assignment + "\n"
}
