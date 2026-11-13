package cli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	latestReleaseAPI = "https://api.github.com/repos/heartleo/zlib/releases/latest"
	maxReleaseJSON   = 1 << 20
	maxChecksums     = 1 << 20
	maxArchive       = 100 << 20
	maxBinary        = 200 << 20
)

type releaseUpdater struct {
	client *http.Client
	apiURL string
	// onEvent, when set, receives each step as it happens so a renderer can
	// show progress. It is called from the goroutine running update.
	onEvent func(updateEvent)
}

type updateEventKind int

const (
	updateChecked updateEventKind = iota
	updateDownloadStarted
	updateDownloadProgress
	updateDownloaded
	updateVerified
	updateInstalled
)

// updateEvent reports one step of a release update.
type updateEvent struct {
	kind    updateEventKind
	version string        // latest release tag
	archive string        // release archive being downloaded
	written int64         // bytes downloaded so far
	total   int64         // expected archive size, or -1 when the server omits it
	elapsed time.Duration // time the finished step took
}

// progressInterval limits download progress events to a rate a terminal can
// redraw smoothly; the final count is always reported by updateDownloaded.
const progressInterval = 50 * time.Millisecond

func (u releaseUpdater) emit(e updateEvent) {
	if u.onEvent != nil {
		u.onEvent(e)
	}
}

type updateResult struct {
	version string
	updated bool
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func newReleaseUpdater() releaseUpdater {
	return releaseUpdater{client: &http.Client{Timeout: 2 * time.Minute}, apiURL: latestReleaseAPI}
}

func (u releaseUpdater) update(ctx context.Context, executable, currentVersion, goos, goarch string) (updateResult, error) {
	var release githubRelease
	checkStart := time.Now()
	metadata, err := u.get(ctx, u.apiURL, maxReleaseJSON)
	if err != nil {
		return updateResult{}, fmt.Errorf("check latest release: %w", err)
	}
	if err := json.Unmarshal(metadata, &release); err != nil {
		return updateResult{}, fmt.Errorf("parse latest release: %w", err)
	}
	if !semver.IsValid(release.TagName) {
		return updateResult{}, fmt.Errorf("invalid latest release tag %q", release.TagName)
	}
	result := updateResult{version: release.TagName}
	u.emit(updateEvent{kind: updateChecked, version: release.TagName, elapsed: time.Since(checkStart)})
	if semver.Compare(currentVersion, release.TagName) >= 0 {
		return result, nil
	}

	archiveName, binaryName, err := releaseArtifact(release.TagName, goos, goarch)
	if err != nil {
		return updateResult{}, err
	}
	archiveURL, checksumsURL := "", ""
	for _, asset := range release.Assets {
		switch asset.Name {
		case archiveName:
			archiveURL = asset.URL
		case "checksums.txt":
			checksumsURL = asset.URL
		}
	}
	if archiveURL == "" || checksumsURL == "" {
		return updateResult{}, fmt.Errorf("release %s is missing %s or checksums.txt", release.TagName, archiveName)
	}
	checksums, err := u.get(ctx, checksumsURL, maxChecksums)
	if err != nil {
		return updateResult{}, fmt.Errorf("download release checksums: %w", err)
	}
	expected, err := releaseChecksum(checksums, archiveName)
	if err != nil {
		return updateResult{}, err
	}

	archiveFile, err := os.CreateTemp("", "zlib-release-*")
	if err != nil {
		return updateResult{}, err
	}
	defer os.Remove(archiveFile.Name())
	defer archiveFile.Close()
	// Hash while downloading so verification needs no second read.
	actual := sha256.New()
	if err := u.download(ctx, archiveURL, archiveName, io.MultiWriter(archiveFile, actual), maxArchive); err != nil {
		return updateResult{}, fmt.Errorf("download %s: %w", archiveName, err)
	}
	if !strings.EqualFold(hex.EncodeToString(actual.Sum(nil)), expected) {
		return updateResult{}, fmt.Errorf("checksum mismatch for %s", archiveName)
	}
	u.emit(updateEvent{kind: updateVerified, archive: archiveName})
	installStart := time.Now()

	info, err := os.Stat(executable)
	if err != nil {
		return updateResult{}, fmt.Errorf("inspect running executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return updateResult{}, fmt.Errorf("running executable is not a regular file: %s", executable)
	}
	pattern := ".zlib-update-*"
	if goos == "windows" {
		pattern += ".exe"
	}
	staged, err := os.CreateTemp(filepath.Dir(executable), pattern)
	if err != nil {
		return updateResult{}, fmt.Errorf("stage update beside executable: %w", err)
	}
	defer os.Remove(staged.Name())
	if _, err := archiveFile.Seek(0, io.SeekStart); err != nil {
		staged.Close()
		return updateResult{}, err
	}
	if err := extractReleaseBinary(archiveFile, archiveName, binaryName, staged); err != nil {
		staged.Close()
		return updateResult{}, err
	}
	if err := staged.Chmod(info.Mode().Perm()); err != nil {
		staged.Close()
		return updateResult{}, err
	}
	if err := staged.Close(); err != nil {
		return updateResult{}, err
	}
	if err := replaceExecutable(staged.Name(), executable); err != nil {
		return updateResult{}, err
	}
	result.updated = true
	u.emit(updateEvent{kind: updateInstalled, version: release.TagName, elapsed: time.Since(installStart)})
	return result, nil
}

func releaseArtifact(tag, goos, goarch string) (archive, binary string, err error) {
	arch := map[string]string{"amd64": "x86_64", "arm64": "arm64"}[goarch]
	if arch == "" || (goos != "linux" && goos != "darwin" && goos != "windows") {
		return "", "", fmt.Errorf("no zlib release for %s/%s", goos, goarch)
	}
	version := strings.TrimPrefix(tag, "v")
	archive = fmt.Sprintf("zlib_%s_%s_%s.tar.gz", version, goos, arch)
	binary = "zlib"
	if goos == "windows" {
		archive = strings.TrimSuffix(archive, ".tar.gz") + ".zip"
		binary += ".exe"
	}
	return archive, binary, nil
}

func releaseChecksum(data []byte, filename string) (string, error) {
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != filename {
			continue
		}
		if len(fields[0]) != sha256.Size*2 {
			return "", fmt.Errorf("invalid checksum for %s", filename)
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", fmt.Errorf("invalid checksum for %s: %w", filename, err)
		}
		return fields[0], nil
	}
	return "", fmt.Errorf("checksum for %s not found", filename)
}

func (u releaseUpdater) get(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "zlib-update")
	response, err := u.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", url, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func (u releaseUpdater) download(ctx context.Context, url, name string, destination io.Writer, maxBytes int64) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "zlib-update")
	start := time.Now()
	response, err := u.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned HTTP %d", url, response.StatusCode)
	}
	total := response.ContentLength
	u.emit(updateEvent{kind: updateDownloadStarted, archive: name, total: total})
	counter := &progressCounter{report: func(written int64) {
		u.emit(updateEvent{kind: updateDownloadProgress, archive: name, written: written, total: total})
	}}
	n, err := io.Copy(io.MultiWriter(destination, counter), io.LimitReader(response.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if n > maxBytes {
		return fmt.Errorf("archive exceeds %d bytes", maxBytes)
	}
	u.emit(updateEvent{kind: updateDownloaded, archive: name, written: n, total: total, elapsed: time.Since(start)})
	return nil
}

// progressCounter counts bytes written and reports the running total at most
// once per progressInterval.
type progressCounter struct {
	written int64
	last    time.Time
	report  func(int64)
}

func (c *progressCounter) Write(p []byte) (int, error) {
	c.written += int64(len(p))
	if now := time.Now(); now.Sub(c.last) >= progressInterval {
		c.last = now
		c.report(c.written)
	}
	return len(p), nil
}

func extractReleaseBinary(archiveFile *os.File, archiveName, binaryName string, destination io.Writer) error {
	if strings.HasSuffix(archiveName, ".zip") {
		info, err := archiveFile.Stat()
		if err != nil {
			return err
		}
		archive, err := zip.NewReader(archiveFile, info.Size())
		if err != nil {
			return err
		}
		for _, entry := range archive.File {
			if entry.Name != binaryName {
				continue
			}
			if entry.UncompressedSize64 > maxBinary {
				return fmt.Errorf("binary exceeds %d bytes", maxBinary)
			}
			reader, err := entry.Open()
			if err != nil {
				return err
			}
			defer reader.Close()
			return copyReleaseBinary(destination, reader)
		}
	} else {
		reader, err := gzip.NewReader(archiveFile)
		if err != nil {
			return err
		}
		defer reader.Close()
		archive := tar.NewReader(reader)
		for {
			entry, err := archive.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return err
			}
			if path.Clean(entry.Name) == binaryName && entry.Typeflag == tar.TypeReg {
				if entry.Size > maxBinary {
					return fmt.Errorf("binary exceeds %d bytes", maxBinary)
				}
				return copyReleaseBinary(destination, archive)
			}
		}
	}
	return fmt.Errorf("%s not found in release archive", binaryName)
}

func copyReleaseBinary(destination io.Writer, source io.Reader) error {
	n, err := io.Copy(destination, io.LimitReader(source, maxBinary+1))
	if err != nil {
		return err
	}
	if n == 0 || n > maxBinary {
		return fmt.Errorf("invalid release binary size: %d", n)
	}
	return nil
}

// replaceExecutable swaps staged into place. The old executable is renamed to
// a backup first so a failed install can put it back; once the install has
// succeeded the backup is removed. Windows cannot delete the running
// executable, so there the removal fails quietly and the next update clears
// the leftover.
func replaceExecutable(staged, executable string) error {
	backup := executable + ".previous"
	if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous update backup: %w", err)
	}
	if err := os.Rename(executable, backup); err != nil {
		return fmt.Errorf("back up executable: %w", err)
	}
	if err := os.Rename(staged, executable); err != nil {
		if restoreErr := os.Rename(backup, executable); restoreErr != nil {
			return errors.Join(fmt.Errorf("install update: %w", err), fmt.Errorf("restore executable from %s: %w", backup, restoreErr))
		}
		return fmt.Errorf("install update (original restored): %w", err)
	}
	_ = os.Remove(backup)
	return nil
}
