package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

func TestReleaseArtifact(t *testing.T) {
	tests := []struct{ goos, arch, archive, binary string }{
		{"linux", "amd64", "zlib_0.0.8_linux_x86_64.tar.gz", "zlib"},
		{"darwin", "arm64", "zlib_0.0.8_darwin_arm64.tar.gz", "zlib"},
		{"windows", "amd64", "zlib_0.0.8_windows_x86_64.zip", "zlib.exe"},
	}
	for _, tt := range tests {
		archive, binary, err := releaseArtifact("v0.0.8", tt.goos, tt.arch)
		if err != nil || archive != tt.archive || binary != tt.binary {
			t.Fatalf("releaseArtifact(%s, %s) = %q, %q, %v", tt.goos, tt.arch, archive, binary, err)
		}
	}
	if _, _, err := releaseArtifact("v0.0.8", "freebsd", "amd64"); err == nil {
		t.Fatal("unsupported platform was accepted")
	}
}

func TestUpdateMethodCommands(t *testing.T) {
	for _, tt := range []struct {
		method updateMethod
		name   string
		args   string
	}{
		{updateBrew, "brew", "upgrade heartleo/tap/zlib-cli"},
		{updateWinget, "winget", "upgrade --id heartleo.zlib --exact"},
		{updateGo, "go", "install " + goInstallPackage},
	} {
		name, args := updateCommand(tt.method)
		if name != tt.name || strings.Join(args, " ") != tt.args {
			t.Fatalf("command for %s = %q %q", tt.method, name, args)
		}
	}
	for _, path := range []string{
		"/opt/homebrew/Cellar/zlib-cli/0.0.14/bin/zlib",
		"/usr/local/Cellar/zlib-cli/0.0.14/bin/zlib",
	} {
		if got := detectUpdateMethod(t.Context(), path); got != updateBrew {
			t.Fatalf("brew path %q detected as %s", path, got)
		}
	}
	if got := detectUpdateMethod(t.Context(), `C:\Users\test\AppData\Local\Microsoft\WinGet\Packages\heartleo.zlib_123\zlib.exe`); got != updateWinget {
		t.Fatalf("winget path detected as %s", got)
	}
}

func TestIsModuleBuild(t *testing.T) {
	build := func(path, version string) *debug.BuildInfo {
		return &debug.BuildInfo{Path: path, Main: debug.Module{Path: "github.com/heartleo/zlib/internal/zlib", Version: version}}
	}
	for _, tt := range []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want bool
	}{
		{"go install @latest", build("github.com/heartleo/zlib/cmd/zlib", "v0.0.10"), true, true},
		{"pseudo-version", build("github.com/heartleo/zlib/cmd/zlib", "v0.0.11-0.20261019105317-fb0e24860942"), true, true},
		{"devel build", build("github.com/heartleo/zlib/cmd/zlib", "(devel)"), true, false},
		{"other main package", build("example.com/other", "v1.0.0"), true, false},
		{"no build info", nil, false, false},
	} {
		if got := isModuleBuild(tt.info, tt.ok); got != tt.want {
			t.Errorf("%s: isModuleBuild = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestReleaseUpdateChecksAndReplacesExecutable(t *testing.T) {
	for _, tt := range []struct {
		name, goos, archive string
	}{
		{"tarball", "linux", "zlib_0.0.8_linux_x86_64.tar.gz"},
		{"zip", "windows", "zlib_0.0.8_windows_x86_64.zip"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			binaryName := "zlib"
			if tt.goos == "windows" {
				binaryName += ".exe"
			}
			payload := testReleaseArchive(t, tt.goos, binaryName, []byte("new executable"))
			server := testReleaseServer(t, tt.archive, payload, false)
			defer server.Close()
			executable := filepath.Join(t.TempDir(), binaryName)
			if err := os.WriteFile(executable, []byte("old executable"), 0o755); err != nil {
				t.Fatal(err)
			}
			updater := releaseUpdater{client: server.Client(), apiURL: server.URL + "/latest"}
			result, err := updater.update(t.Context(), executable, "v0.0.7", tt.goos, "amd64")
			if err != nil || !result.updated || result.version != "v0.0.8" {
				t.Fatalf("update = %+v, %v", result, err)
			}
			updated, err := os.ReadFile(executable)
			if err != nil || string(updated) != "new executable" {
				t.Fatalf("updated executable = %q, %v", updated, err)
			}
			if _, err := os.Stat(executable + ".previous"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("backup left next to the executable after a successful update: %v", err)
			}
			result, err = updater.update(t.Context(), executable, "v0.0.8", tt.goos, "amd64")
			if err != nil || result.updated {
				t.Fatalf("same-version update = %+v, %v", result, err)
			}
		})
	}
}

func TestReleaseUpdateRejectsBadChecksumWithoutReplacing(t *testing.T) {
	archive := "zlib_0.0.8_linux_x86_64.tar.gz"
	server := testReleaseServer(t, archive, testReleaseArchive(t, "linux", "zlib", []byte("new")), true)
	defer server.Close()
	executable := filepath.Join(t.TempDir(), "zlib")
	if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	updater := releaseUpdater{client: server.Client(), apiURL: server.URL + "/latest"}
	if _, err := updater.update(t.Context(), executable, "v0.0.7", "linux", "amd64"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("bad checksum error = %v", err)
	}
	data, err := os.ReadFile(executable)
	if err != nil || string(data) != "old" {
		t.Fatalf("original executable changed: %q, %v", data, err)
	}
}

func TestReplaceExecutableRestoresOriginalAfterFailedInstall(t *testing.T) {
	executable := filepath.Join(t.TempDir(), "zlib")
	if err := os.WriteFile(executable, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceExecutable(filepath.Join(t.TempDir(), "missing"), executable); err == nil {
		t.Fatal("missing staged file should fail")
	}
	data, err := os.ReadFile(executable)
	if err != nil || string(data) != "old" {
		t.Fatalf("rollback failed: %q, %v", data, err)
	}
	if _, err := os.Stat(executable + ".previous"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup left after the restore: %v", err)
	}
}

func TestReplaceExecutableClearsStaleBackup(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "zlib")
	staged := filepath.Join(dir, "staged")
	for path, content := range map[string]string{executable: "old", staged: "new", executable + ".previous": "stale"} {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := replaceExecutable(staged, executable); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(executable); err != nil || string(data) != "new" {
		t.Fatalf("executable = %q, %v", data, err)
	}
	if _, err := os.Stat(executable + ".previous"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale backup survived: %v", err)
	}
}

func testReleaseArchive(t *testing.T, goos, binaryName string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if goos == "windows" {
		writer := zip.NewWriter(&buffer)
		file, err := writer.Create(binaryName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(content); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
	} else {
		gzipWriter := gzip.NewWriter(&buffer)
		tarWriter := tar.NewWriter(gzipWriter)
		if err := tarWriter.WriteHeader(&tar.Header{Name: binaryName, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(content); err != nil {
			t.Fatal(err)
		}
		if err := tarWriter.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gzipWriter.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return buffer.Bytes()
}

func testReleaseServer(t *testing.T, archiveName string, archive []byte, badChecksum bool) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	hash := sha256.Sum256(archive)
	checksum := hex.EncodeToString(hash[:])
	if badChecksum {
		checksum = strings.Repeat("0", sha256.Size*2)
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v0.0.8",
				"assets": []map[string]string{
					{"name": archiveName, "browser_download_url": server.URL + "/archive"},
					{"name": "checksums.txt", "browser_download_url": server.URL + "/checksums"},
				},
			})
		case "/archive":
			_, _ = w.Write(archive)
		case "/checksums":
			fmt.Fprintf(w, "%s  %s\n", checksum, archiveName)
		default:
			http.NotFound(w, r)
		}
	}))
	return server
}
