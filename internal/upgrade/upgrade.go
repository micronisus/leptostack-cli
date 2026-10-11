// Package upgrade implements self-update from GitHub releases: version checks
// (cached 24h) and atomic binary replacement with sha256 verification.
package upgrade

import (
	"archive/tar"
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

	url, err := assetURL(rel, assetName())
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
		return fmt.Errorf("writing new binary (try running with sudo if %s is not writable): %w", self, err)
	}
	if err := os.Rename(tmp, self); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replacing %s: %w", self, err)
	}
	io.Errf("Updated leptostack to v%s. Re-run your command.\n", latest)
	return nil
}

func assetName() string {
	return fmt.Sprintf("leptostack_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
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

func extractBinary(data []byte, name string) ([]byte, error) {
	if !strings.HasSuffix(name, ".tar.gz") {
		return data, nil
	}
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
		if hdr.Typeflag == tar.TypeReg && (hdr.Name == "leptostack" || strings.HasSuffix(hdr.Name, "/leptostack")) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("leptostack binary not found in archive")
}
