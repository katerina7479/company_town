package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/katerina7479/company_town/internal/session"
)

const defaultUpdateRepo = "katerina7479/company_town"

// UpdateOptions holds the parsed flags for the update command.
type UpdateOptions struct {
	Check      bool // --check: print available version and exit, no install
	Force      bool // --force: reinstall even if already at latest
	Prerelease bool // --prerelease: include prerelease versions
}

// updateDeps holds injectable dependencies for testing.
type updateDeps struct {
	httpGet    func(url string) ([]byte, error)
	executable func() (string, error)
	evalLinks  func(path string) (string, error)
	stat       func(path string) (os.FileInfo, error)
	rename     func(oldpath, newpath string) error
	tempDir    func() string
	sessions   func() ([]string, error)
	goos       string
	goarch     string
}

var defaultUpdateDeps = updateDeps{
	httpGet:    httpGetBody,
	executable: os.Executable,
	evalLinks:  filepath.EvalSymlinks,
	stat:       os.Stat,
	rename:     os.Rename,
	tempDir:    os.TempDir,
	sessions:   session.ListCompanyTown,
	goos:       runtime.GOOS,
	goarch:     runtime.GOARCH,
}

type githubRelease struct {
	TagName    string         `json:"tag_name"`
	Prerelease bool           `json:"prerelease"`
	Assets     []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Update implements the self-update logic used by both ct and gt.
// currentVersion is the running binary's version (from the main.version ldflag).
func Update(currentVersion string, opts UpdateOptions) error {
	return updateWith(currentVersion, opts, defaultUpdateDeps)
}

func updateWith(currentVersion string, opts UpdateOptions, deps updateDeps) error {
	repo := os.Getenv("CT_UPDATE_REPO")
	if repo == "" {
		repo = defaultUpdateRepo
	}
	apiBase := os.Getenv("CT_UPDATE_URL")
	if apiBase == "" {
		apiBase = "https://api.github.com/repos/" + repo
	}

	release, err := fetchLatestRelease(apiBase, opts.Prerelease, deps.httpGet)
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(currentVersion, "v")

	if opts.Check {
		fmt.Printf("current: %s\nlatest:  %s\n", currentVersion, release.TagName)
		if current == latest {
			fmt.Println("already up to date")
		}
		return nil
	}

	if current == latest && !opts.Force {
		fmt.Printf("ct/gt already at %s\n", release.TagName)
		return nil
	}

	// Locate both binaries relative to the running executable.
	self, err := deps.executable()
	if err != nil {
		return fmt.Errorf("locating running binary: %w", err)
	}
	self, err = deps.evalLinks(self)
	if err != nil {
		return fmt.Errorf("resolving symlink for running binary: %w", err)
	}
	dir := filepath.Dir(self)
	ctPath := filepath.Join(dir, "ct")
	gtPath := filepath.Join(dir, "gt")
	for _, p := range []string{ctPath, gtPath} {
		if _, statErr := deps.stat(p); os.IsNotExist(statErr) {
			return fmt.Errorf("sibling binary not found at %s — both ct and gt must live in the same directory", p)
		}
	}

	goos := deps.goos
	if goos == "" {
		goos = runtime.GOOS
	}
	goarch := deps.goarch
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	assetName := assetFilename(latest, goos, goarch)
	tarAsset, err := findAsset(release.Assets, assetName)
	if err != nil {
		return err
	}
	checksumAsset, err := findAsset(release.Assets, "checksums.txt")
	if err != nil {
		return err
	}

	fmt.Printf("downloading %s...\n", assetName)
	tarData, err := deps.httpGet(tarAsset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("downloading tarball: %w", err)
	}
	checksumData, err := deps.httpGet(checksumAsset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("downloading checksums: %w", err)
	}

	if err := verifyChecksum(tarData, assetName, checksumData); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	ctData, gtData, err := extractBinaries(tarData)
	if err != nil {
		return fmt.Errorf("extracting binaries: %w", err)
	}

	// Warn if Company Town sessions are running — existing processes continue
	// using the old in-memory binary; only new invocations pick up the update.
	if sessions, _ := deps.sessions(); len(sessions) > 0 {
		fmt.Println("note: Company Town sessions are running — run 'ct stop && ct start' to pick up the new binary")
	}

	if err := atomicReplace(ctPath, ctData, deps.tempDir(), deps.stat, deps.rename); err != nil {
		return fmt.Errorf("replacing ct: %w", err)
	}
	if err := atomicReplace(gtPath, gtData, deps.tempDir(), deps.stat, deps.rename); err != nil {
		return fmt.Errorf("replacing gt: %w", err)
	}

	if current == latest {
		fmt.Printf("ct/gt: %s reinstalled (--force)\n", release.TagName)
	} else {
		fmt.Printf("ct/gt: v%s → %s\n", current, release.TagName)
	}
	return nil
}

// fetchLatestRelease returns the most recent applicable release from the
// GitHub Releases API. When prerelease is false, the /releases/latest
// endpoint is used (always stable). When prerelease is true, the first entry
// from /releases (sorted newest-first) is returned.
func fetchLatestRelease(apiBase string, prerelease bool, get func(string) ([]byte, error)) (*githubRelease, error) {
	if !prerelease {
		data, err := get(apiBase + "/releases/latest")
		if err != nil {
			return nil, err
		}
		var r githubRelease
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("parsing release: %w", err)
		}
		return &r, nil
	}

	data, err := get(apiBase + "/releases?per_page=10")
	if err != nil {
		return nil, err
	}
	var releases []githubRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, fmt.Errorf("parsing releases: %w", err)
	}
	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases found")
	}
	return &releases[0], nil
}

// assetFilename constructs the expected tarball filename for the given version,
// OS, and arch using the goreleaser naming convention for this project.
func assetFilename(version, goos, goarch string) string {
	return fmt.Sprintf("company_town_%s_%s_%s.tar.gz", version, goos, goarch)
}

func findAsset(assets []releaseAsset, name string) (releaseAsset, error) {
	for _, a := range assets {
		if a.Name == name {
			return a, nil
		}
	}
	return releaseAsset{}, fmt.Errorf("release asset %q not found", name)
}

// verifyChecksum checks that the SHA-256 of data matches the entry for
// assetName in the checksums file (format: "<hex>  <filename>" per line).
func verifyChecksum(data []byte, assetName string, checksums []byte) error {
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, line := range strings.Split(string(checksums), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == assetName {
			if parts[0] != sum {
				return fmt.Errorf("want %s, got %s", parts[0], sum)
			}
			return nil
		}
	}
	return fmt.Errorf("no checksum entry for %q in checksums.txt", assetName)
}

// extractBinaries reads a .tar.gz archive and returns the raw bytes of the
// ct and gt binaries contained within.
func extractBinaries(data []byte) (ct, gt []byte, err error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, nil, fmt.Errorf("opening gzip: %w", err)
	}
	defer gr.Close() //nolint:errcheck

	tr := tar.NewReader(gr)
	found := map[string][]byte{}
	for {
		hdr, tarErr := tr.Next()
		if tarErr == io.EOF {
			break
		}
		if tarErr != nil {
			return nil, nil, fmt.Errorf("reading tar: %w", tarErr)
		}
		base := filepath.Base(hdr.Name)
		if base == "ct" || base == "gt" {
			b, readErr := io.ReadAll(tr)
			if readErr != nil {
				return nil, nil, fmt.Errorf("reading %s from archive: %w", base, readErr)
			}
			found[base] = b
		}
	}
	if len(found["ct"]) == 0 || len(found["gt"]) == 0 {
		return nil, nil, fmt.Errorf("ct and/or gt not found in archive")
	}
	return found["ct"], found["gt"], nil
}

// atomicReplace writes data to a temp file in tmpDir, then renames it over dst.
// On permission-denied errors a helpful message is included in the returned error.
func atomicReplace(dst string, data []byte, tmpDir string, statFn func(string) (os.FileInfo, error), renameFn func(string, string) error) error {
	info, err := statFn(dst)
	if err != nil {
		return err
	}
	perm := info.Mode().Perm()

	tmp, err := os.CreateTemp(tmpDir, "ct-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := renameFn(tmpName, dst); err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied writing to %s — try: sudo ct update", dst)
		}
		return err
	}
	return nil
}

// httpGetBody performs a GET and returns the response body.
func httpGetBody(url string) ([]byte, error) {
	resp, err := http.Get(url) //nolint:gosec,noctx
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}
