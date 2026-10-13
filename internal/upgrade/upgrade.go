// Package upgrade implements self-update from GitHub releases: version checks
// (cached 24h) and atomic binary replacement with sha256 verification.
package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/micronisus/leptostack-cli/internal/cli"
	"github.com/micronisus/leptostack-cli/internal/version"
)

// ReleasesURL is the human-facing releases page.
const ReleasesURL = "https://github.com/micronisus/leptostack-cli/releases"

// latestAPI is the GitHub API endpoint for the latest release.
var latestAPI = "https://api.github.com/repos/micronisus/leptostack-cli/releases/latest"

// cacheFile is the cached latest-version filename inside the config dir.
const cacheFile = "latest_version"

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Latest fetches the latest release tag from GitHub.
func Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if r.TagName == "" {
		return "", fmt.Errorf("could not determine the latest version")
	}
	return strings.TrimPrefix(r.TagName, "v"), nil
}

// CachedLatest returns the cached latest version if it is younger than maxAge.
func CachedLatest(configDir string, maxAge time.Duration) (string, bool) {
	path := filepath.Join(configDir, cacheFile)
	info, err := os.Stat(path)
	if err != nil || time.Since(info.ModTime()) > maxAge {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	v := strings.TrimSpace(string(b))
	return v, v != ""
}

// WriteCache stores the latest version for later checks.
func WriteCache(configDir, latest string) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(configDir, cacheFile), []byte(latest+"\n"), 0o644)
}

// Check reports the latest version and whether an update is available.
func Check(ctx context.Context, configDir, current string, refresh bool) (string, bool, error) {
	latest := ""
	if !refresh {
		if v, ok := CachedLatest(configDir, 24*time.Hour); ok {
			latest = v
		}
	}
	if latest == "" {
		v, err := Latest(ctx)
		if err != nil {
			if cached, ok := CachedLatest(configDir, 365*24*time.Hour); ok {
				latest = cached
			} else {
				return "", false, err
			}
		} else {
			latest = v
			_ = WriteCache(configDir, v)
		}
	}
	latest = strings.TrimPrefix(latest, "v")
	if current == "dev" || current == "" {
		return latest, false, nil
	}
	return latest, version.ParseSemver(strings.TrimPrefix(current, "v")).Compare(version.ParseSemver(latest)) < 0, nil
}

// Run downloads and installs the latest release.
func Run(ctx context.Context, io *cli.IOStreams, current string) error {
	CleanupOld()
	io.Errln("Checking for the latest version...")
	rel, err := fetchRelease(ctx)
	if err != nil {
		return err
	}
	latest := strings.TrimPrefix(rel.TagName, "v")
	if latest == "" {
		return fmt.Errorf("could not determine the latest version")
	}
	if latest == strings.TrimPrefix(current, "v") {
		io.Errf("Already up to date (v%s).\n", latest)
		return nil
	}

	url, err := assetURL(rel, assetName(runtime.GOOS, runtime.GOARCH))
	if err != nil {
		return err
	}
	sumsURL, ok := assetURLOrEmpty(rel, "checksums.txt")
	if !ok {
		return fmt.Errorf("no checksums.txt published for v%s; refusing to install unverified", latest)
	}

	io.Errf("Downloading %s...\n", url)
	data, err := download(ctx, url)
	if err != nil {
		return err
	}

	if err := verifyChecksum(ctx, data, sumsURL, filepath.Base(url)); err != nil {
		return err
	}
	io.Errln("Checksum verified.")

	bin, err := extractBinary(data, filepath.Base(url))
	if err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	self, _ = filepath.EvalSymlinks(self)
	tmp := self + ".new"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return fmt.Errorf("writing new binary (try running with elevated permissions if %s is not writable): %w", self, err)
	}
	if err := replaceExecutable(tmp, self); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	io.Errf("Updated leptostack to v%s. Re-run your command.\n", latest)
	return nil
}

// CleanupOld removes a leftover ".old" binary from a previous Windows upgrade.
// It is safe to call on every start and is a no-op when there is nothing to
// clean or on platforms that replace the binary atomically.
func CleanupOld() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	self, _ = filepath.EvalSymlinks(self)
	_ = os.Remove(self + ".old")
}

// assetName returns the release archive name for a platform. Windows ships a
// .zip (so install.ps1/upgrade avoid tar), every other platform a .tar.gz.
func assetName(goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("leptostack_%s_%s%s", goos, goarch, ext)
}

func assetURL(rel *release, name string) (string, error) {
	url, ok := assetURLOrEmpty(rel, name)
	if !ok {
		return "", fmt.Errorf("no release asset %q in %s", name, rel.TagName)
	}
	return url, nil
}

func assetURLOrEmpty(rel *release, name string) (string, bool) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a.BrowserDownloadURL, true
		}
	}
	return "", false
}

func fetchRelease(ctx context.Context) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r release
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func verifyChecksum(ctx context.Context, data []byte, sumsURL, assetName string) error {
	sums, err := download(ctx, sumsURL)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == assetName {
			if !strings.EqualFold(fields[0], want) {
				return fmt.Errorf("sha256 mismatch for %s (want %s, got %s)", assetName, fields[0], want)
			}
			return nil
		}
	}
	return fmt.Errorf("no checksum entry for %s", assetName)
}

// extractBinary returns the leptostack binary from a release archive. It
// dispatches on the archive extension; an unknown extension is treated as a raw
// binary for forward compatibility.
func extractBinary(data []byte, name string) ([]byte, error) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".tar.gz"):
		return extractTarGz(data)
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(data)
	default:
		return data, nil
	}
}

// isBinaryName reports whether a name inside an archive is the leptostack
// binary ("leptostack" or "leptostack.exe"), ignoring case and any directory
// prefix.
func isBinaryName(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return base == "leptostack" || base == "leptostack.exe"
}

// extractTarGz extracts the leptostack binary from a .tar.gz archive.
func extractTarGz(data []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg && isBinaryName(hdr.Name) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("leptostack binary not found in archive")
}

// extractZip extracts the leptostack binary from a .zip archive.
func extractZip(data []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !isBinaryName(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("leptostack binary not found in archive")
}
