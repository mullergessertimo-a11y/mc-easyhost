package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// The app checks GitHub for a newer release and can replace itself: the new
// file is downloaded next to the running one, checked against GitHub's
// SHA-256, swapped in, and the app restarts into it.

var releasesAPI = "https://api.github.com/repos/mullergessertimo-a11y/mc-easyhost/releases/latest"

func init() {
	// Lets a development copy test the updater against a local fake release.
	if u := os.Getenv("EASYHOST_RELEASES_URL"); u != "" {
		releasesAPI = u
	}
}

type UpdateInfo struct {
	Current    string `json:"current"`
	Latest     string `json:"latest"`
	Available  bool   `json:"available"`
	CanInstall bool   `json:"canInstall"` // false: download it by hand
	Notes      string `json:"notes"`
	Page       string `json:"page"`
	Error      string `json:"error,omitempty"`
}

type Updater struct {
	mu      sync.Mutex
	info    UpdateInfo
	checked time.Time
	asset   struct{ URL, SHA256, Name string }
	busy    bool
}

// releaseAsset is the download for this system.
func releaseAsset() string {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return "MC-EasyHost.exe"
	case "linux/amd64":
		return "mc-easyhost-linux-x86_64.tar.gz"
	case "linux/arm64":
		return "mc-easyhost-linux-aarch64.tar.gz"
	}
	return ""
}

func isNewer(latest, current string) bool {
	l, c := strings.TrimPrefix(latest, "v"), strings.TrimPrefix(current, "v")
	return compareVersions(l, c) > 0
}

// Check asks GitHub at most every few hours; force skips the cache.
func (u *Updater) Check(force bool) UpdateInfo {
	u.mu.Lock()
	defer u.mu.Unlock()
	if version == "dev" {
		return UpdateInfo{Current: version}
	}
	if !force && time.Since(u.checked) < 6*time.Hour && u.info.Current != "" {
		return u.info
	}
	var rel struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Draft   bool   `json:"draft"`
		Pre     bool   `json:"prerelease"`
		Assets  []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	info := UpdateInfo{Current: version}
	u.checked = time.Now()
	if err := getJSON(releasesAPI, &rel); err != nil {
		info.Error = "could not check for updates"
		u.info = info
		u.checked = time.Now().Add(-6*time.Hour + 10*time.Minute) // try again soon
		return info
	}
	info.Latest, info.Notes, info.Page = rel.TagName, rel.Body, rel.HTMLURL
	info.Available = !rel.Draft && !rel.Pre && isNewer(rel.TagName, version)
	want := releaseAsset()
	for _, a := range rel.Assets {
		if a.Name == want && strings.HasPrefix(a.Digest, "sha256:") {
			u.asset.URL, u.asset.SHA256, u.asset.Name = a.URL, strings.TrimPrefix(a.Digest, "sha256:"), a.Name
			info.CanInstall = info.Available
		}
	}
	u.info = info
	return info
}

// Install downloads the new version and swaps it in. The caller restarts.
func (u *Updater) Install(dataDir string) (string, error) {
	info := u.Check(true)
	u.mu.Lock()
	if !info.CanInstall {
		u.mu.Unlock()
		return "", errors.New("no update to install")
	}
	if u.busy {
		u.mu.Unlock()
		return "", errors.New("the update is already being installed")
	}
	u.busy = true
	asset := u.asset
	u.mu.Unlock()
	defer func() { u.mu.Lock(); u.busy = false; u.mu.Unlock() }()

	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	fresh := exe + ".new"
	if f, err := os.Create(fresh); err != nil {
		return "", fmt.Errorf("this copy can't update itself (%v). Download the new version from GitHub instead", err)
	} else {
		f.Close()
	}
	defer os.Remove(fresh)

	if strings.HasSuffix(asset.Name, ".tar.gz") {
		tmp := filepath.Join(dataDir, "tmp", "update")
		os.RemoveAll(tmp)
		defer os.RemoveAll(tmp)
		archive := filepath.Join(tmp, asset.Name)
		if err := downloadFile(asset.URL, archive, "sha256", asset.SHA256, nil); err != nil {
			return "", fmt.Errorf("downloading the update failed: %w", err)
		}
		if err := extractTarGz(archive, tmp); err != nil {
			return "", err
		}
		bin := filepath.Join(tmp, strings.TrimSuffix(asset.Name, ".tar.gz"), "mc-easyhost")
		if err := copyFile(bin, fresh); err != nil {
			return "", fmt.Errorf("the update package looks wrong: %w", err)
		}
	} else if err := downloadFile(asset.URL, fresh, "sha256", asset.SHA256, nil); err != nil {
		return "", fmt.Errorf("downloading the update failed: %w", err)
	}
	os.Chmod(fresh, 0o755)

	// A running program can be renamed (also on Windows), not overwritten.
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return "", fmt.Errorf("could not replace the app: %w", err)
	}
	if err := os.Rename(fresh, exe); err != nil {
		os.Rename(old, exe)
		return "", fmt.Errorf("could not replace the app: %w", err)
	}
	return exe, nil
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}

// cleanupOldBinary removes the previous version left by an update. On
// Windows it stays locked until the old process has fully exited.
func cleanupOldBinary() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	for i := 0; i < 10; i++ {
		if err := os.Remove(exe + ".old"); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(2 * time.Second)
	}
}
