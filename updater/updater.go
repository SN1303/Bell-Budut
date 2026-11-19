// Package updater implements OTA updates from GitHub Releases.
//
// Flow: Check() asks the GitHub API for the latest release, Apply() downloads
// the release zip, verifies its SHA-256 checksum, replaces bell-budut.exe and
// the web/ folder next to the executable, and Restart() starts the new exe.
// The data/ folder (database, uploaded audio) is never touched.
package updater

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"bell-budut/version"
)

type Release struct {
	Version     string `json:"version"`
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Notes       string `json:"notes"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
	AssetURL    string `json:"-"`
	AssetSize   int64  `json:"asset_size"`
	ChecksumURL string `json:"-"`
}

var (
	cacheMu   sync.Mutex
	cached    *Release
	cachedAt  time.Time
	applyMu   sync.Mutex
	userAgent = "bell-budut-updater"
)

const cacheTTL = 10 * time.Minute

// Check returns the latest published release (cached for a few minutes to respect GitHub rate limits).
func Check(ctx context.Context, force bool) (*Release, error) {
	cacheMu.Lock()
	if !force && cached != nil && time.Since(cachedAt) < cacheTTL {
		r := *cached
		cacheMu.Unlock()
		return &r, nil
	}
	cacheMu.Unlock()

	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", version.Repo)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tidak bisa menghubungi GitHub (cek koneksi internet): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, errors.New("belum ada rilis (Release) di GitHub")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub menjawab status %d", resp.StatusCode)
	}

	var gh struct {
		TagName     string `json:"tag_name"`
		Name        string `json:"name"`
		Body        string `json:"body"`
		PublishedAt string `json:"published_at"`
		HTMLURL     string `json:"html_url"`
		Draft       bool   `json:"draft"`
		Prerelease  bool   `json:"prerelease"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		return nil, fmt.Errorf("respon GitHub tidak valid: %w", err)
	}

	rel := &Release{
		Version:     strings.TrimPrefix(strings.TrimSpace(gh.TagName), "v"),
		Tag:         gh.TagName,
		Name:        gh.Name,
		Notes:       gh.Body,
		PublishedAt: gh.PublishedAt,
		HTMLURL:     gh.HTMLURL,
	}
	for _, a := range gh.Assets {
		switch a.Name {
		case version.AssetName:
			rel.AssetURL = a.URL
			rel.AssetSize = a.Size
		case version.AssetName + ".sha256":
			rel.ChecksumURL = a.URL
		}
	}

	cacheMu.Lock()
	cached = rel
	cachedAt = time.Now()
	cacheMu.Unlock()

	r := *rel
	return &r, nil
}

// IsNewer reports whether version a is newer than version b ("4.10.0" > "4.9.2").
func IsNewer(a, b string) bool {
	pa, pb := parseVer(a), parseVer(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseVer(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	for i, p := range strings.SplitN(v, ".", 3) {
		n, _ := strconv.Atoi(p)
		out[i] = n
	}
	return out
}

// AppDir is the folder that holds bell-budut.exe (and web/, data/).
func AppDir() (string, string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe), exe, nil
}

// CleanupOld removes leftovers of a previous update (old exe, temp files).
func CleanupOld() {
	dir, exe, err := AppDir()
	if err != nil {
		return
	}
	_ = os.Remove(exe + ".old")
	_ = os.RemoveAll(filepath.Join(dir, ".update_tmp"))
}

// Apply downloads and installs the release. On success the caller must Restart().
func Apply(ctx context.Context, rel *Release) error {
	if !applyMu.TryLock() {
		return errors.New("proses update sedang berjalan")
	}
	defer applyMu.Unlock()

	if rel == nil || rel.AssetURL == "" {
		return fmt.Errorf("file rilis %s tidak ditemukan di Release GitHub", version.AssetName)
	}

	dir, exe, err := AppDir()
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, ".update_tmp")
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	// 1. Download zip
	zipPath := filepath.Join(tmp, version.AssetName)
	sum, err := download(ctx, rel.AssetURL, zipPath)
	if err != nil {
		return fmt.Errorf("gagal mengunduh update: %w", err)
	}

	// 2. Verify checksum (when published)
	if rel.ChecksumURL != "" {
		want, err := fetchChecksum(ctx, rel.ChecksumURL)
		if err != nil {
			return fmt.Errorf("gagal mengambil checksum: %w", err)
		}
		if !strings.EqualFold(want, sum) {
			return errors.New("checksum file update tidak cocok, update dibatalkan")
		}
	}

	// 3. Extract (only bell-budut.exe and web/**)
	stage := filepath.Join(tmp, "stage")
	if err := extract(zipPath, stage); err != nil {
		return fmt.Errorf("gagal mengekstrak update: %w", err)
	}
	newExe := filepath.Join(stage, "bell-budut.exe")
	if st, err := os.Stat(newExe); err != nil || st.Size() < 1024 {
		return errors.New("paket update tidak berisi bell-budut.exe yang valid")
	}

	// 4. Replace web/ files (overwrite in place, keep files that are not in the package)
	stageWeb := filepath.Join(stage, "web")
	if _, err := os.Stat(stageWeb); err == nil {
		if err := copyTree(stageWeb, filepath.Join(dir, "web")); err != nil {
			return fmt.Errorf("gagal memperbarui folder web: %w", err)
		}
	}

	// 5. Swap the executable (a running exe on Windows can be renamed, not overwritten)
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return fmt.Errorf("gagal menyiapkan penggantian exe: %w", err)
	}
	if err := moveFile(newExe, exe); err != nil {
		_ = os.Rename(old, exe) // roll back
		return fmt.Errorf("gagal memasang exe baru: %w", err)
	}
	return nil
}

// Restart starts the (new) executable with the same arguments. The caller must then exit
// so the new process can take over the HTTP port.
func Restart() error {
	dir, exe, err := AppDir()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Dir = dir
	setDetached(cmd)
	return cmd.Start()
}

func download(ctx context.Context, url, dst string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", userAgent)
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	f, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 500<<20)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func fetchChecksum(ctx context.Context, url string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return "", errors.New("checksum kosong")
	}
	return fields[0], nil
}

func extract(zipPath, dst string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	base, _ := filepath.Abs(dst)
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if name != "bell-budut.exe" && !strings.HasPrefix(name, "web/") {
			continue // never touch data/, scripts, etc.
		}
		target := filepath.Join(base, filepath.FromSlash(name))
		if !strings.HasPrefix(target, base+string(os.PathSeparator)) {
			return fmt.Errorf("path tidak aman di dalam zip: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 500<<20))
		rc.Close()
		out.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}
