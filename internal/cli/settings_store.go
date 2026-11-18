package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/heartleo/zlib/internal/config"
	"github.com/heartleo/zlib/internal/metasource"
	zlib "github.com/heartleo/zlib/internal/zlib"
	"github.com/joho/godotenv"
)

// settingKeys lists the settings backed by .env files and the environment, in
// the order store.values returns them. New keys go at the end: a few pages
// still index values by position.
var settingKeys = []string{zlib.EnvDomain, zlib.EnvProxy, downloadDirEnvVar, zlib.EnvRetries, searchExtensionsEnv, wereadProxyEnv, googleBooksKeyEnv, metaSourcesEnv, metaResultsEnv, nameLayoutEnv, wereadAPIKeyEnv}

// metaSourcesByLanguage is what the sources row shows when no list is set: the
// sources follow the book's language.
const metaSourcesByLanguage = "By book language"

// googleKeyNotSet is what the key row shows when there is no key. It is not a
// value: displaySetting leaves it alone and masks everything else.
const googleKeyNotSet = "Not set (shared quota)"

// wereadKeyNotSet is the same for the WeRead API key row.
const wereadKeyNotSet = "Not set"

// isSecretSetting reports whether a setting is an API key: it is shown masked
// everywhere, its editor hides what is typed and opens empty, and it is removed
// only on request.
func isSecretSetting(key string) bool { return key == googleBooksKeyEnv || key == wereadAPIKeyEnv }

var settingsEnvironment map[string]string

// settingEnvNames is every name the settings read: the keys plus the
// Google Books key alias, which has no row of its own.
func settingEnvNames() []string { return append(slices.Clone(settingKeys), googleBooksKeyAliasEnv) }

func captureSettingsEnvironment() {
	settingsEnvironment = make(map[string]string)
	for _, key := range settingEnvNames() {
		if value, ok := os.LookupEnv(key); ok {
			settingsEnvironment[key] = value
		}
	}
}

type settingValue struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Source   string `json:"source"`
	Saved    string `json:"saved"`
	Override string `json:"override,omitempty"`

	// alias is set when the value came from googleBooksKeyAliasEnv, so there
	// is nothing under Key to remove.
	alias bool
}

type settingsStore struct {
	global, local string
	kindle        string
	env           map[string]string
}

func newSettingsStore() (settingsStore, error) {
	global, err := globalDotEnvPath()
	if err != nil {
		return settingsStore{}, err
	}
	wd, err := os.Getwd()
	if err != nil {
		return settingsStore{}, err
	}
	env := settingsEnvironment
	if env == nil {
		env = make(map[string]string)
		for _, k := range settingEnvNames() {
			if v, ok := os.LookupEnv(k); ok {
				env[k] = v
			}
		}
	}
	return settingsStore{global: global, local: filepath.Join(wd, ".env"), kindle: config.KindleConfigPath(), env: env}, nil
}

func readSettingsEnv(path string) (map[string]string, error) {
	v, err := godotenv.Read(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	return v, err
}

func (s settingsStore) values() ([]settingValue, error) {
	g, err := readSettingsEnv(s.global)
	if err != nil {
		return nil, err
	}
	l, err := readSettingsEnv(s.local)
	if err != nil {
		return nil, err
	}
	if globalInfo, e := os.Stat(s.global); e == nil {
		if localInfo, e := os.Stat(s.local); e == nil && os.SameFile(globalInfo, localInfo) {
			l = map[string]string{}
		}
	}
	var values []settingValue
	for _, key := range settingKeys {
		v := settingValue{Key: key, Source: "Default", Saved: g[key]}
		if x, ok := s.env[key]; ok {
			v.Value, v.Source = x, "Process environment"
		} else if x, ok := l[key]; ok {
			v.Value, v.Source = x, "Working-directory .env"
		} else if x, ok := g[key]; ok {
			v.Value, v.Source = x, "Global configuration"
		}
		if v.Source == "Process environment" || v.Source == "Working-directory .env" {
			v.Override = v.Source
		}
		if key == zlib.EnvRetries && strings.TrimSpace(v.Value) != "" {
			if n, e := strconv.Atoi(strings.TrimSpace(v.Value)); e != nil || n < 0 {
				v.Value = "3"
				v.Source = "Default (invalid retry setting)"
			}
		}
		if key == googleBooksKeyEnv && strings.TrimSpace(v.Value) == "" {
			// A key saved or exported under the plain name counts when
			// nothing holds the ZLIB_ one; the same order as the lookups use.
			for _, a := range []struct {
				from map[string]string
				name string
			}{{s.env, "Process environment"}, {l, "Working-directory .env"}, {g, "Global configuration"}} {
				if x := a.from[googleBooksKeyAliasEnv]; strings.TrimSpace(x) != "" {
					v.Value, v.Source, v.alias = x, a.name+" ("+googleBooksKeyAliasEnv+")", true
					break
				}
			}
		}
		if strings.TrimSpace(v.Value) == "" {
			switch key {
			case zlib.EnvDomain:
				if session, e := config.LoadSession(); e == nil && session.Domain != "" {
					v.Value, v.Source = session.Domain, "Saved session"
				}
			case zlib.EnvProxy, wereadProxyEnv:
				v.Value, v.Source = "System proxy environment", "Default"
			case downloadDirEnvVar:
				v.Value, v.Source = "Current directory", "Default"
			case zlib.EnvRetries:
				v.Value, v.Source = "3", "Default"
			case searchExtensionsEnv:
				v.Value, v.Source = "Any format", "Default"
			case googleBooksKeyEnv:
				v.Value, v.Source = googleKeyNotSet, "Default"
			case wereadAPIKeyEnv:
				v.Value, v.Source = wereadKeyNotSet, "Default"
			case metaSourcesEnv:
				v.Value, v.Source = metaSourcesByLanguage, "Default"
			case metaResultsEnv:
				v.Value, v.Source = strconv.Itoa(metasource.DefaultMaxResults), "Default"
			case nameLayoutEnv:
				v.Value, v.Source = string(metasource.DefaultNameLayout), "Default"
			}
		}
		values = append(values, v)
	}
	return values, nil
}

func displaySetting(key, value string) string {
	// An API key is a credential: every screen and --json shows it masked.
	if isSecretSetting(key) {
		if strings.TrimSpace(value) == "" || value == googleKeyNotSet || value == wereadKeyNotSet {
			return value
		}
		return "Configured (hidden)"
	}
	if key == wereadProxyEnv && strings.EqualFold(strings.TrimSpace(value), wereadProxyDirect) {
		return "Direct (no proxy)"
	}
	if key == zlib.EnvProxy || key == wereadProxyEnv {
		if strings.TrimSpace(value) == "" {
			return ""
		}
		if u, e := url.Parse(value); e == nil && u.User != nil {
			u.User = url.User("***")
			return u.String()
		}
		if strings.Contains(value, "@") {
			return "Configured proxy (credentials hidden)"
		}
	}
	return bookCell(value)
}

func validateSetting(key, value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("enter a single-line value")
	}
	if key == kindleSMTPPasswordEnv {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("enter an SMTP password")
		}
		return value, nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	switch key {
	case zlib.EnvDomain:
		return zlib.ParseAllowedDomain(value)
	case zlib.EnvProxy:
		if _, err := validateDoctorProxy(value); err != nil {
			return "", err
		}
	case wereadProxyEnv:
		return validateWeReadProxy(value)
	case googleBooksKeyEnv:
		// The message never repeats the value: it is a credential.
		if !metasource.ValidAPIKey(value) {
			return "", fmt.Errorf("a Google API key is 20 to 128 letters, digits, - and _")
		}
	case wereadAPIKeyEnv:
		// The message never repeats the value: it is a credential.
		if !metasource.ValidWeReadKey(value) {
			return "", fmt.Errorf("a WeRead API key is 8 to 128 letters, digits, - and _")
		}
	case metaSourcesEnv:
		return metasource.NormalizeSourceList(value)
	case nameLayoutEnv:
		l, err := metasource.ParseNameLayout(value)
		if err != nil {
			return "", err
		}
		return string(l), nil
	case metaResultsEnv:
		n, err := metasource.ParseMaxResults(value)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(n), nil
	case downloadDirEnvVar:
		path, err := expandTilde(value)
		if err != nil {
			return "", err
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return "", err
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("directory does not exist")
		}
		if !info.IsDir() {
			return "", fmt.Errorf("choose a directory")
		}
		value = path
	case zlib.EnvRetries:
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return "", fmt.Errorf("enter a non-negative integer")
		}
		value = strconv.Itoa(n)
	case searchExtensionsEnv:
		extensions, err := parseDefaultSearchExtensions(value)
		if err != nil {
			return "", err
		}
		formats := make([]string, len(extensions))
		for i, ext := range extensions {
			formats[i] = strings.ToLower(ext.String())
		}
		value = strings.Join(formats, ",")
	default:
		return "", fmt.Errorf("unknown setting")
	}
	return value, nil
}

func (s settingsStore) save(key, value string) error {
	value, err := validateSetting(key, value)
	if err != nil {
		return err
	}
	original, err := os.ReadFile(s.global)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := readSettingsEnv(s.global); err != nil {
		return err
	}
	pattern := regexp.MustCompile(`^\s*(?:export\s+)?` + regexp.QuoteMeta(key) + `\s*=`)
	newline := "\n"
	if strings.Contains(string(original), "\r\n") {
		newline = "\r\n"
	}
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(string(original), "\r\n", "\n"), "\n") {
		if !pattern.MatchString(line) {
			lines = append(lines, line)
		}
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if value != "" {
		encoded := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`).Replace(value)
		lines = append(lines, key+`="`+encoded+`"`)
	}
	if err := os.MkdirAll(filepath.Dir(s.global), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.global), ".env-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.WriteString(strings.Join(lines, newline) + newline); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.global)
}
