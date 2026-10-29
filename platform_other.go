//go:build !windows

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func initPlatform() {}

func setupChildProcess(cmd *exec.Cmd) { setParentDeathSignal(cmd) }

func attachChildProcess(cmd *exec.Cmd) {}

// defaultDataDir follows the XDG spec on Linux (worlds and Java are data,
// not config), e.g. ~/.local/share/mc-easyhost.
func defaultDataDir() string {
	if runtime.GOOS == "linux" {
		if d := os.Getenv("XDG_DATA_HOME"); d != "" {
			return filepath.Join(d, "mc-easyhost")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".local", "share", "mc-easyhost")
		}
	}
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "MC EasyHost")
	}
	return "mc-easyhost-data"
}

func totalMemoryMB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if fields := strings.Fields(sc.Text()); len(fields) >= 2 && fields[0] == "MemTotal:" {
			kb, _ := strconv.Atoi(fields[1])
			return kb / 1024
		}
	}
	return 0
}

// openAppWindow prefers a Chromium-based browser in --app mode, which looks
// like a normal desktop window; otherwise it opens a tab in the default browser.
func openAppWindow(url, profileDir string) {
	browsers := []string{
		"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
		"microsoft-edge", "microsoft-edge-stable", "brave-browser", "vivaldi-stable",
	}
	for _, b := range browsers {
		if p, err := exec.LookPath(b); err == nil {
			cmd := exec.Command(p, "--app="+url, "--user-data-dir="+profileDir,
				"--window-size=1180,820", "--no-first-run", "--no-default-browser-check", "--class=mc-easyhost")
			if cmd.Start() == nil {
				go cmd.Wait()
				return
			}
		}
	}
	exec.Command("xdg-open", url).Start()
}

func openFolder(path string) { exec.Command("xdg-open", path).Start() }
