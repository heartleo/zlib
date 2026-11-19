// Command mcpb builds what a release gives MCP clients: one MCP Bundle (.mcpb)
// per platform, and the server.json the official MCP Registry publishes, which
// lists those bundles with their SHA-256 hashes.
//
//	go run ./scripts/mcpb bundle --binary dist/zlib --os linux --arch amd64 --version 0.0.15 --out dist/mcpb
//	go run ./scripts/mcpb server --version 0.0.15 --assets dist/mcpb --out server.json
//
// GoReleaser runs bundle after each build (see .goreleaser.yml); the registry
// workflow runs server against the bundles of a published release.
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultManifest = "packaging/mcpb/manifest.json"
	defaultTemplate = "packaging/mcp/server.json"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "mcpb:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: mcpb bundle|server [flags]")
	}
	switch args[0] {
	case "bundle":
		fs := flag.NewFlagSet("bundle", flag.ContinueOnError)
		binary := fs.String("binary", "", "the built zlib executable")
		goos := fs.String("os", "", "its GOOS: linux, darwin or windows")
		goarch := fs.String("arch", "", "its GOARCH: amd64 or arm64")
		version := fs.String("version", "", "the release version, without the v")
		manifest := fs.String("manifest", defaultManifest, "the manifest template")
		license := fs.String("license", "LICENSE", "the license file to include")
		out := fs.String("out", "dist/mcpb", "the directory for the bundle")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		file, err := buildBundle(bundleOptions{*binary, *goos, *goarch, *version, *manifest, *license, *out})
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, file)
		return nil
	case "server":
		fs := flag.NewFlagSet("server", flag.ContinueOnError)
		version := fs.String("version", "", "the release version, without the v")
		assets := fs.String("assets", "dist/mcpb", "the directory holding the release's bundles")
		template := fs.String("template", defaultTemplate, "the server.json template")
		out := fs.String("out", "server.json", "where to write server.json")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return writeServerJSON(*template, *assets, *version, *out)
	}
	return fmt.Errorf("unknown command %q (use bundle or server)", args[0])
}

// platform is a release target: the GOOS and GOARCH names, the platform name
// MCPB uses, and the architecture as release asset names spell it.
type platform struct{ goos, goarch, mcpb, arch string }

// platforms are the targets .goreleaser.yml builds, in the order the registry
// lists them.
var platforms = []platform{
	{"linux", "amd64", "linux", "x86_64"},
	{"linux", "arm64", "linux", "arm64"},
	{"darwin", "amd64", "darwin", "x86_64"},
	{"darwin", "arm64", "darwin", "arm64"},
	{"windows", "amd64", "win32", "x86_64"},
	{"windows", "arm64", "win32", "arm64"},
}

func findPlatform(goos, goarch string) (platform, error) {
	for _, p := range platforms {
		if p.goos == goos && p.goarch == goarch {
			return p, nil
		}
	}
	return platform{}, fmt.Errorf("no bundle for %s/%s", goos, goarch)
}

// bundleName is the file name of a bundle, spelled like the release's archives.
func bundleName(version string, p platform) string {
	return "zlib_" + version + "_" + p.goos + "_" + p.arch + ".mcpb"
}

// versionRe is a release version: semantic, without the v, as the registry and
// the manifest both take it.
var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z][0-9A-Za-z.-]*)?$`)

// manifest is the part of manifest.json this tool changes and checks; the rest
// passes through as it was written.
type manifest struct {
	ManifestVersion string          `json:"manifest_version"`
	Name            string          `json:"name"`
	DisplayName     string          `json:"display_name"`
	Version         string          `json:"version"`
	Description     string          `json:"description"`
	LongDescription string          `json:"long_description"`
	Author          json.RawMessage `json:"author"`
	Repository      json.RawMessage `json:"repository"`
	Homepage        string          `json:"homepage"`
	Documentation   string          `json:"documentation"`
	Support         string          `json:"support"`
	License         string          `json:"license"`
	Keywords        []string        `json:"keywords"`
	PrivacyPolicies []string        `json:"privacy_policies"`
	Server          manifestServer  `json:"server"`
	Tools           json.RawMessage `json:"tools"`
	UserConfig      json.RawMessage `json:"user_config"`
	Compatibility   *compatibility  `json:"compatibility,omitempty"`
}

type manifestServer struct {
	Type       string    `json:"type"`
	EntryPoint string    `json:"entry_point"`
	MCPConfig  mcpConfig `json:"mcp_config"`
}

type mcpConfig struct {
	Command           string                       `json:"command"`
	Args              []string                     `json:"args"`
	Env               map[string]string            `json:"env,omitempty"`
	PlatformOverrides map[string]map[string]string `json:"platform_overrides,omitempty"`
}

type compatibility struct {
	Platforms []string `json:"platforms"`
}

type bundleOptions struct {
	binary, goos, goarch, version, manifest, license, out string
}

// buildBundle writes the bundle for one platform: manifest.json, the license and
// the executable under server/. It returns the bundle's path. The archive is
// reproducible: fixed order, fixed times, and the executable marked executable.
func buildBundle(o bundleOptions) (string, error) {
	if !versionRe.MatchString(o.version) {
		return "", fmt.Errorf("--version %q is not a release version such as 0.0.15", o.version)
	}
	p, err := findPlatform(o.goos, o.goarch)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(o.manifest)
	if err != nil {
		return "", err
	}
	var m manifest
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return "", fmt.Errorf("%s: %w", o.manifest, err)
	}
	exe := "zlib"
	if p.goos == "windows" {
		exe = "zlib.exe"
	}
	m.Version = o.version
	m.Compatibility = &compatibility{Platforms: []string{p.mcpb}}
	m.Server.EntryPoint = "server/" + exe
	m.Server.MCPConfig.Command = "${__dirname}/server/" + exe
	if err := checkManifest(m); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	binary, err := os.ReadFile(o.binary)
	if err != nil {
		return "", err
	}
	license, err := os.ReadFile(o.license)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return "", err
	}
	file := filepath.Join(o.out, bundleName(o.version, p))
	f, err := os.Create(file)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(f)
	for _, e := range []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{"manifest.json", append(data, '\n'), 0o644},
		{"LICENSE", license, 0o644},
		{path.Join("server", exe), binary, 0o755},
	} {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)}
		h.SetMode(e.mode)
		w, err := zw.CreateHeader(h)
		if err == nil {
			_, err = w.Write(e.data)
		}
		if err != nil {
			f.Close()
			return "", err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return "", err
	}
	return file, f.Close()
}

// checkManifest refuses a manifest a client would reject or that would leave the
// bundle unusable.
func checkManifest(m manifest) error {
	switch {
	case m.ManifestVersion == "" || m.Name == "" || m.Description == "" || len(m.Author) == 0:
		return errors.New("manifest: manifest_version, name, description and author are required")
	case m.Server.Type != "binary" || m.Server.MCPConfig.Command == "" || len(m.Server.MCPConfig.Args) == 0 || m.Server.MCPConfig.Args[0] != "mcp":
		return errors.New(`manifest: the server must be a binary run as "zlib mcp"`)
	case len(m.PrivacyPolicies) == 0:
		return errors.New("manifest: privacy_policies is required, since the server talks to external services")
	}
	return nil
}

// Registry limits, from the server.json schema.
const (
	maxNameLen        = 200
	maxDescriptionLen = 100
	maxVersionLen     = 255
)

var (
	registryNameRe = regexp.MustCompile(`^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$`)
	sha256Re       = regexp.MustCompile(`^[a-f0-9]{64}$`)
	// versionRangeRe matches what looks like a range: the registry refuses it.
	versionRangeRe = regexp.MustCompile(`^[\^~<>=]|(^|\.)[xX*]($|\.)|\s|\|\|`)
)

// serverJSON is server.json as the registry takes it.
type serverJSON struct {
	Schema      string          `json:"$schema"`
	Name        string          `json:"name"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description"`
	WebsiteURL  string          `json:"websiteUrl,omitempty"`
	Repository  json.RawMessage `json:"repository,omitempty"`
	Icons       json.RawMessage `json:"icons,omitempty"`
	Version     string          `json:"version"`
	Packages    []registryPkg   `json:"packages"`
}

type registryPkg struct {
	RegistryType         string          `json:"registryType"`
	Identifier           string          `json:"identifier"`
	Version              string          `json:"version,omitempty"`
	FileSha256           string          `json:"fileSha256,omitempty"`
	Transport            json.RawMessage `json:"transport"`
	EnvironmentVariables json.RawMessage `json:"environmentVariables,omitempty"`
}

// validateServer checks s against the schema's constraints and the registry's
// package rules: what the registry would answer with a 4xx, found before
// publishing. releaseRepo is the repository the packages must be hosted in.
func validateServer(s serverJSON, releaseRepo string) error {
	switch {
	case len(s.Name) < 3 || len(s.Name) > maxNameLen || !registryNameRe.MatchString(s.Name):
		return fmt.Errorf("name %q must be reverse-DNS with one slash, such as io.github.user/server", s.Name)
	case s.Description == "" || len([]rune(s.Description)) > maxDescriptionLen:
		return fmt.Errorf("description must be 1 to %d characters, got %d", maxDescriptionLen, len([]rune(s.Description)))
	case s.Title != "" && len([]rune(s.Title)) > 100:
		return errors.New("title must be at most 100 characters")
	case s.Version == "" || len(s.Version) > maxVersionLen || versionRangeRe.MatchString(s.Version):
		return fmt.Errorf("version %q must be one specific version, not a range", s.Version)
	}
	if !strings.HasPrefix(s.Name, "io.github.") {
		return fmt.Errorf("name %q: GitHub authentication needs the io.github.<owner>/ prefix", s.Name)
	}
	for i, p := range s.Packages {
		where := fmt.Sprintf("packages[%d]", i)
		u, err := url.Parse(p.Identifier)
		switch {
		case p.RegistryType != "mcpb":
			return fmt.Errorf("%s: registryType %q: only mcpb bundles are built", where, p.RegistryType)
		case err != nil || u.Scheme != "https" || u.Host != "github.com" || !strings.HasPrefix(u.Path, "/"+releaseRepo+"/releases/download/"):
			return fmt.Errorf("%s: the identifier must be a download URL of a release of %s on GitHub", where, releaseRepo)
		case !strings.Contains(strings.ToLower(p.Identifier), "mcp"):
			return fmt.Errorf("%s: the identifier URL must contain \"mcp\"", where)
		case !sha256Re.MatchString(p.FileSha256):
			return fmt.Errorf("%s: fileSha256 must be 64 lower-case hex digits", where)
		case p.Version != "":
			return fmt.Errorf("%s: an mcpb package has no version of its own", where)
		case len(p.Transport) == 0:
			return fmt.Errorf("%s: transport is required", where)
		}
	}
	return nil
}

// repoPath is "owner/name" of the repository a server.json names.
func repoPath(s serverJSON) (string, error) {
	var r struct{ URL string }
	if err := json.Unmarshal(s.Repository, &r); err != nil || r.URL == "" {
		return "", errors.New("repository.url is required")
	}
	u, err := url.Parse(r.URL)
	if err != nil || u.Host != "github.com" {
		return "", fmt.Errorf("repository.url %q must be on github.com", r.URL)
	}
	return strings.Trim(u.Path, "/"), nil
}

// writeServerJSON renders the template for a release: its version, and one
// package per bundle in assets with the bundle's SHA-256. Every platform must
// have a bundle, so the registry never lists one that does not exist.
func writeServerJSON(templatePath, assets, version, out string) error {
	if !versionRe.MatchString(version) {
		return fmt.Errorf("--version %q is not a release version such as 0.0.15", version)
	}
	raw, err := os.ReadFile(templatePath)
	if err != nil {
		return err
	}
	var s serverJSON
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("%s: %w", templatePath, err)
	}
	if len(s.Packages) != 1 {
		return fmt.Errorf("%s: keep exactly one prototype package, copied for each bundle", templatePath)
	}
	repo, err := repoPath(s)
	if err != nil {
		return err
	}
	proto := s.Packages[0]
	s.Version, s.Packages = version, nil
	for _, p := range platforms {
		name := bundleName(version, p)
		sum, err := fileSHA256(filepath.Join(assets, name))
		if err != nil {
			return fmt.Errorf("the release has no usable bundle %s: %w", name, err)
		}
		pkg := proto
		pkg.Identifier = "https://github.com/" + repo + "/releases/download/v" + version + "/" + name
		pkg.FileSha256 = sum
		s.Packages = append(s.Packages, pkg)
	}
	if err := validateServer(s, repo); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, append(data, '\n'), 0o644)
}

func fileSHA256(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
