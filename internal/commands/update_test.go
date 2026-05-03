package commands

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// buildTarGz creates an in-memory .tar.gz archive containing the given files
// (name → content).
func buildTarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, data := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0755,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeChecksums(data []byte, name string) []byte {
	sum := sha256.Sum256(data)
	return []byte(fmt.Sprintf("%x  %s\n", sum, name))
}

func makeRelease(tag string, assets []releaseAsset) githubRelease {
	return githubRelease{TagName: tag, Assets: assets}
}

func releaseJSON(r githubRelease) []byte {
	b, _ := json.Marshal(r)
	return b
}

// TestAssetFilename verifies the goreleaser naming convention.
func TestAssetFilename(t *testing.T) {
	got := assetFilename("1.2.3", "linux", "amd64")
	want := "company_town_1.2.3_linux_amd64.tar.gz"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// testOS/testArch are injected into deps so asset names in tests are stable
// regardless of the host machine.
const testOS = "linux"
const testArch = "amd64"

// TestVerifyChecksum covers match, mismatch, and missing-entry cases.
func TestVerifyChecksum(t *testing.T) {
	data := []byte("binary content")
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	checksums := []byte(sum + "  myfile.tar.gz\n")

	if err := verifyChecksum(data, "myfile.tar.gz", checksums); err != nil {
		t.Errorf("expected ok, got: %v", err)
	}

	bad := []byte("tampered")
	if err := verifyChecksum(bad, "myfile.tar.gz", checksums); err == nil {
		t.Error("expected checksum mismatch error")
	}

	if err := verifyChecksum(data, "other.tar.gz", checksums); err == nil {
		t.Error("expected missing-entry error")
	}
}

// TestExtractBinaries verifies that ct and gt are correctly pulled out of a tarball.
func TestExtractBinaries(t *testing.T) {
	ctBytes := []byte("ct binary")
	gtBytes := []byte("gt binary")
	tarball := buildTarGz(t, map[string][]byte{
		"company_town_1.0.0_darwin_arm64/ct": ctBytes,
		"company_town_1.0.0_darwin_arm64/gt": gtBytes,
		"company_town_1.0.0_darwin_arm64/LICENSE": []byte("MIT"),
	})

	ct, gt, err := extractBinaries(tarball)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(ct, ctBytes) {
		t.Errorf("ct mismatch: got %q, want %q", ct, ctBytes)
	}
	if !bytes.Equal(gt, gtBytes) {
		t.Errorf("gt mismatch: got %q, want %q", gt, gtBytes)
	}
}

// TestExtractBinaries_missingBinary verifies a clear error when a binary is absent.
func TestExtractBinaries_missingBinary(t *testing.T) {
	tarball := buildTarGz(t, map[string][]byte{
		"ct": []byte("ct binary"),
		// gt intentionally absent
	})
	if _, _, err := extractBinaries(tarball); err == nil {
		t.Error("expected error when gt is missing from archive")
	}
}

// TestFindAsset verifies asset lookup by name.
func TestFindAsset(t *testing.T) {
	assets := []releaseAsset{
		{Name: "company_town_1.0.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/a"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/c"},
	}

	a, err := findAsset(assets, "checksums.txt")
	if err != nil || a.BrowserDownloadURL != "https://example.com/c" {
		t.Errorf("expected to find checksums.txt: err=%v asset=%v", err, a)
	}

	_, err = findAsset(assets, "nonexistent.tar.gz")
	if err == nil {
		t.Error("expected error for missing asset")
	}
}

// TestUpdateWith_alreadyCurrent verifies a no-op when versions match.
func TestUpdateWith_alreadyCurrent(t *testing.T) {
	release := makeRelease("v1.0.0", nil)
	deps := updateDeps{
		httpGet: func(url string) ([]byte, error) {
			return releaseJSON(release), nil
		},
	}

	err := updateWith("v1.0.0", UpdateOptions{}, deps)
	if err != nil {
		t.Errorf("expected no-op, got: %v", err)
	}
}

// TestUpdateWith_check verifies --check prints and exits without modifying anything.
func TestUpdateWith_check(t *testing.T) {
	release := makeRelease("v2.0.0", nil)
	httpCalls := 0
	deps := updateDeps{
		httpGet: func(url string) ([]byte, error) {
			httpCalls++
			return releaseJSON(release), nil
		},
		executable: func() (string, error) { return "", fmt.Errorf("should not be called") },
	}

	err := updateWith("v1.0.0", UpdateOptions{Check: true}, deps)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if httpCalls != 1 {
		t.Errorf("expected 1 HTTP call (releases/latest), got %d", httpCalls)
	}
}

// TestUpdateWith_happyPath verifies the full update flow with mocked deps.
func TestUpdateWith_happyPath(t *testing.T) {
	dir := t.TempDir()

	ctPath := filepath.Join(dir, "ct")
	gtPath := filepath.Join(dir, "gt")
	for _, p := range []string{ctPath, gtPath} {
		if err := os.WriteFile(p, []byte("old"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	ctData := []byte("new ct binary")
	gtData := []byte("new gt binary")
	assetName := assetFilename("2.0.0", testOS, testArch)
	tarball := buildTarGz(t, map[string][]byte{"ct": ctData, "gt": gtData})
	checksums := makeChecksums(tarball, assetName)

	release := makeRelease("v2.0.0", []releaseAsset{
		{Name: assetName, BrowserDownloadURL: "https://example.com/tarball"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
	})

	deps := updateDeps{
		httpGet: func(url string) ([]byte, error) {
			switch url {
			case "https://api.github.com/repos/katerina7479/company_town/releases/latest":
				return releaseJSON(release), nil
			case "https://example.com/tarball":
				return tarball, nil
			case "https://example.com/checksums":
				return checksums, nil
			}
			return nil, fmt.Errorf("unexpected URL: %s", url)
		},
		executable: func() (string, error) { return filepath.Join(dir, "ct"), nil },
		evalLinks:  func(p string) (string, error) { return p, nil },
		stat:       os.Stat,
		rename:     os.Rename,
		tempDir:    func() string { return dir },
		sessions:   func() ([]string, error) { return nil, nil },
		goos:       testOS,
		goarch:     testArch,
	}

	if err := updateWith("v1.0.0", UpdateOptions{}, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	gotCT, _ := os.ReadFile(ctPath)
	gotGT, _ := os.ReadFile(gtPath)
	if !bytes.Equal(gotCT, ctData) {
		t.Errorf("ct: got %q, want %q", gotCT, ctData)
	}
	if !bytes.Equal(gotGT, gtData) {
		t.Errorf("gt: got %q, want %q", gotGT, gtData)
	}
}

// TestUpdateWith_checksumMismatch verifies that a bad checksum aborts before any rename.
func TestUpdateWith_checksumMismatch(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{filepath.Join(dir, "ct"), filepath.Join(dir, "gt")} {
		if err := os.WriteFile(p, []byte("old"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	assetName := assetFilename("2.0.0", testOS, testArch)
	tarball := buildTarGz(t, map[string][]byte{"ct": []byte("ct"), "gt": []byte("gt")})
	badChecksums := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte("wrong")), assetName))

	release := makeRelease("v2.0.0", []releaseAsset{
		{Name: assetName, BrowserDownloadURL: "https://example.com/tarball"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
	})

	renamed := false
	deps := updateDeps{
		httpGet: func(url string) ([]byte, error) {
			switch url {
			case "https://api.github.com/repos/katerina7479/company_town/releases/latest":
				return releaseJSON(release), nil
			case "https://example.com/tarball":
				return tarball, nil
			case "https://example.com/checksums":
				return badChecksums, nil
			}
			return nil, fmt.Errorf("unexpected URL: %s", url)
		},
		executable: func() (string, error) { return filepath.Join(dir, "ct"), nil },
		evalLinks:  func(p string) (string, error) { return p, nil },
		stat:       os.Stat,
		rename:     func(_, _ string) error { renamed = true; return nil },
		tempDir:    func() string { return dir },
		sessions:   func() ([]string, error) { return nil, nil },
		goos:       testOS,
		goarch:     testArch,
	}

	err := updateWith("v1.0.0", UpdateOptions{}, deps)
	if err == nil {
		t.Fatal("expected checksum error")
	}
	if renamed {
		t.Error("rename must not be called when checksum fails")
	}
}

// TestUpdateWith_siblingMissing verifies an error when one binary is absent.
func TestUpdateWith_siblingMissing(t *testing.T) {
	dir := t.TempDir()
	// Only ct, no gt.
	if err := os.WriteFile(filepath.Join(dir, "ct"), []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	release := makeRelease("v2.0.0", nil)
	deps := updateDeps{
		httpGet: func(url string) ([]byte, error) {
			return releaseJSON(release), nil
		},
		executable: func() (string, error) { return filepath.Join(dir, "ct"), nil },
		evalLinks:  func(p string) (string, error) { return p, nil },
		stat:       os.Stat,
	}

	err := updateWith("v1.0.0", UpdateOptions{}, deps)
	if err == nil {
		t.Fatal("expected error for missing gt binary")
	}
}

// TestUpdateWith_force verifies --force reinstalls even when versions match.
func TestUpdateWith_force(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{filepath.Join(dir, "ct"), filepath.Join(dir, "gt")} {
		if err := os.WriteFile(p, []byte("old"), 0755); err != nil {
			t.Fatal(err)
		}
	}

	assetName := assetFilename("1.0.0", testOS, testArch)
	newBin := []byte("new binary content")
	tarball := buildTarGz(t, map[string][]byte{"ct": newBin, "gt": newBin})
	checksums := makeChecksums(tarball, assetName)

	release := makeRelease("v1.0.0", []releaseAsset{
		{Name: assetName, BrowserDownloadURL: "https://example.com/tarball"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
	})

	renamed := 0
	deps := updateDeps{
		httpGet: func(url string) ([]byte, error) {
			switch url {
			case "https://api.github.com/repos/katerina7479/company_town/releases/latest":
				return releaseJSON(release), nil
			case "https://example.com/tarball":
				return tarball, nil
			case "https://example.com/checksums":
				return checksums, nil
			}
			return nil, fmt.Errorf("unexpected URL: %s", url)
		},
		executable: func() (string, error) { return filepath.Join(dir, "ct"), nil },
		evalLinks:  func(p string) (string, error) { return p, nil },
		stat:       os.Stat,
		rename:     func(old, new string) error { renamed++; return os.Rename(old, new) },
		tempDir:    func() string { return dir },
		sessions:   func() ([]string, error) { return nil, nil },
		goos:       testOS,
		goarch:     testArch,
	}

	if err := updateWith("v1.0.0", UpdateOptions{Force: true}, deps); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if renamed != 2 {
		t.Errorf("expected 2 renames (ct + gt), got %d", renamed)
	}
}

// TestFetchLatestRelease_stable verifies the /releases/latest endpoint is used
// when prerelease is false.
func TestFetchLatestRelease_stable(t *testing.T) {
	release := makeRelease("v1.5.0", nil)
	var calledURL string
	r, err := fetchLatestRelease("https://api.example.com/repos/foo/bar", false,
		func(url string) ([]byte, error) {
			calledURL = url
			return releaseJSON(release), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if r.TagName != "v1.5.0" {
		t.Errorf("got tag %q", r.TagName)
	}
	if calledURL != "https://api.example.com/repos/foo/bar/releases/latest" {
		t.Errorf("unexpected URL: %s", calledURL)
	}
}

// TestFetchLatestRelease_prerelease verifies the /releases list is used and
// the first entry is returned.
func TestFetchLatestRelease_prerelease(t *testing.T) {
	releases := []githubRelease{
		{TagName: "v2.0.0-beta.1", Prerelease: true},
		{TagName: "v1.5.0", Prerelease: false},
	}
	b, _ := json.Marshal(releases)
	r, err := fetchLatestRelease("https://api.example.com/repos/foo/bar", true,
		func(_ string) ([]byte, error) { return b, nil })
	if err != nil {
		t.Fatal(err)
	}
	if r.TagName != "v2.0.0-beta.1" {
		t.Errorf("expected prerelease, got %q", r.TagName)
	}
}
