package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testVersion = "0.0.15"

func fakeBinary(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "zlib")
	if err := os.WriteFile(p, []byte("not really an executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func bundleFor(t *testing.T, goos, goarch, out string) string {
	t.Helper()
	file, err := buildBundle(bundleOptions{
		binary: fakeBinary(t), goos: goos, goarch: goarch, version: testVersion,
		manifest: "../../" + defaultManifest, license: "../../LICENSE", out: out,
	})
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func readZip(t *testing.T, file string) map[string]*zip.File {
	t.Helper()
	zr, err := zip.OpenReader(file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { zr.Close() })
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	return files
}

// A bundle holds the manifest for its platform, the license and the executable,
// and builds to the same bytes every time.
func TestBundleHoldsTheManifestLicenseAndExecutable(t *testing.T) {
	out := t.TempDir()
	for goos, tc := range map[string]struct{ platform, exe string }{
		"linux": {"linux", "server/zlib"}, "darwin": {"darwin", "server/zlib"}, "windows": {"win32", "server/zlib.exe"},
	} {
		file := bundleFor(t, goos, "arm64", out)
		if filepath.Base(file) != "zlib_"+testVersion+"_"+goos+"_arm64.mcpb" {
			t.Errorf("bundle name %q", filepath.Base(file))
		}
		files := readZip(t, file)
		if len(files) != 3 || files["manifest.json"] == nil || files["LICENSE"] == nil || files[tc.exe] == nil {
			t.Fatalf("%s: entries %v", goos, files)
		}
		if mode := files[tc.exe].Mode(); mode&0o111 == 0 {
			t.Errorf("%s: the executable is not marked executable: %v", goos, mode)
		}
		rc, _ := files["manifest.json"].Open()
		data, _ := io.ReadAll(rc)
		rc.Close()
		var m manifest
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if m.Version != testVersion || m.Compatibility == nil || len(m.Compatibility.Platforms) != 1 || m.Compatibility.Platforms[0] != tc.platform ||
			m.Server.EntryPoint != tc.exe || !strings.HasSuffix(m.Server.MCPConfig.Command, "/"+tc.exe) || m.Server.MCPConfig.Args[0] != "mcp" {
			t.Errorf("%s: manifest %+v", goos, m)
		}
	}
	a, _ := os.ReadFile(bundleFor(t, "linux", "amd64", t.TempDir()))
	b, _ := os.ReadFile(bundleFor(t, "linux", "amd64", t.TempDir()))
	if !bytes.Equal(a, b) {
		t.Error("the same inputs built different bundles")
	}
	for _, bad := range []bundleOptions{
		{binary: fakeBinary(t), goos: "plan9", goarch: "amd64", version: testVersion},
		{binary: fakeBinary(t), goos: "linux", goarch: "amd64", version: "v0.0.15"},
		{binary: fakeBinary(t), goos: "linux", goarch: "amd64", version: "^0.0.15"},
	} {
		bad.manifest, bad.license, bad.out = "../../"+defaultManifest, "../../LICENSE", t.TempDir()
		if _, err := buildBundle(bad); err == nil {
			t.Errorf("%+v was built", bad)
		}
	}
}

// server.json lists every platform's bundle with the SHA-256 of the file, and no
// release with a bundle missing is rendered.
func TestServerJSONListsEveryBundleWithItsHash(t *testing.T) {
	assets := t.TempDir()
	for _, p := range platforms {
		bundleFor(t, p.goos, p.goarch, assets)
	}
	out := filepath.Join(t.TempDir(), "server.json")
	if err := writeServerJSON("../../"+defaultTemplate, assets, testVersion, out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(out)
	var s serverJSON
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatal(err)
	}
	if s.Version != testVersion || len(s.Packages) != len(platforms) || s.Name != "io.github.heartleo/zlib" {
		t.Fatalf("server.json = %s", data)
	}
	for i, p := range platforms {
		name := bundleName(testVersion, p)
		sum, err := fileSHA256(filepath.Join(assets, name))
		if err != nil {
			t.Fatal(err)
		}
		pkg := s.Packages[i]
		if pkg.Identifier != "https://github.com/heartleo/zlib/releases/download/v"+testVersion+"/"+name || pkg.FileSha256 != sum || pkg.RegistryType != "mcpb" {
			t.Errorf("package %d = %+v", i, pkg)
		}
		if len(pkg.EnvironmentVariables) == 0 {
			t.Errorf("package %d lost its environment variables", i)
		}
	}
	if err := os.Remove(filepath.Join(assets, bundleName(testVersion, platforms[3]))); err != nil {
		t.Fatal(err)
	}
	if err := writeServerJSON("../../"+defaultTemplate, assets, testVersion, out); err == nil || !strings.Contains(err.Error(), "darwin_arm64") {
		t.Errorf("a release with a missing bundle was rendered: %v", err)
	}
}

// The checks stand for the registry's: what it would refuse is refused here.
func TestValidateServerHoldsTheRegistrysRules(t *testing.T) {
	good := func() serverJSON {
		return serverJSON{
			Name: "io.github.heartleo/zlib", Description: "A server.", Version: "1.2.3",
			Packages: []registryPkg{{
				RegistryType: "mcpb", Transport: json.RawMessage(`{"type":"stdio"}`),
				Identifier: "https://github.com/heartleo/zlib/releases/download/v1.2.3/zlib_1.2.3_linux_x86_64.mcpb",
				FileSha256: strings.Repeat("a", 64),
			}},
		}
	}
	if err := validateServer(good(), "heartleo/zlib"); err != nil {
		t.Fatalf("a good server.json was refused: %v", err)
	}
	for name, mutate := range map[string]func(*serverJSON){
		"no slash":          func(s *serverJSON) { s.Name = "zlib" },
		"not github":        func(s *serverJSON) { s.Name = "com.example/zlib" },
		"long description":  func(s *serverJSON) { s.Description = strings.Repeat("d", 101) },
		"empty description": func(s *serverJSON) { s.Description = "" },
		"range version":     func(s *serverJSON) { s.Version = "^1.2.3" },
		"wildcard":          func(s *serverJSON) { s.Version = "1.x" },
		"other registry":    func(s *serverJSON) { s.Packages[0].RegistryType = "npm" },
		"other repo": func(s *serverJSON) {
			s.Packages[0].Identifier = strings.Replace(s.Packages[0].Identifier, "heartleo/zlib", "evil/zlib", 1)
		},
		"no mcp in url": func(s *serverJSON) {
			s.Packages[0].Identifier = "https://github.com/heartleo/zlib/releases/download/v1/zlib.zip"
		},
		"placeholder sha": func(s *serverJSON) { s.Packages[0].FileSha256 = "PLACEHOLDER_SHA256" },
		"upper sha":       func(s *serverJSON) { s.Packages[0].FileSha256 = strings.Repeat("A", 64) },
		"package version": func(s *serverJSON) { s.Packages[0].Version = "1.2.3" },
	} {
		s := good()
		mutate(&s)
		if err := validateServer(s, "heartleo/zlib"); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// A prerelease and a name ending in x are specific versions, not ranges.
	for _, v := range []string{"0.0.15", "0.0.15-rc.1", "1.0.0-linux"} {
		s := good()
		s.Version = v
		if err := validateServer(s, "heartleo/zlib"); err != nil {
			t.Errorf("%s: %v", v, err)
		}
	}
}

// The committed templates are valid, so a release cannot fail on them.
func TestTemplatesAreValid(t *testing.T) {
	raw, err := os.ReadFile("../../" + defaultTemplate)
	if err != nil {
		t.Fatal(err)
	}
	var s serverJSON
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	repo, err := repoPath(s)
	if err != nil || repo != "heartleo/zlib" {
		t.Fatalf("repo %q, %v", repo, err)
	}
	if err := validateServer(s, repo); err != nil {
		t.Errorf("server.json template: %v", err)
	}
	raw, err = os.ReadFile("../../" + defaultManifest)
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	if err := checkManifest(m); err != nil {
		t.Errorf("manifest template: %v", err)
	}
	if m.Description != s.Description {
		t.Errorf("the descriptions differ:\n%q\n%q", m.Description, s.Description)
	}
	if m.License != "MIT" || m.ManifestVersion != "0.3" {
		t.Errorf("license %q, manifest_version %q", m.License, m.ManifestVersion)
	}
}
