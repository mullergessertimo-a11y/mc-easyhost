//go:build !windows

package main

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Non-Windows builds exist for development and testing.

func initPlatform() {}

func setupChildProcess(cmd *exec.Cmd) { setParentDeathSignal(cmd) }

func attachChildProcess(cmd *exec.Cmd) {}

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

func openAppWindow(url, profileDir string) {
	for _, b := range []string{"microsoft-edge", "google-chrome", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(b); err == nil {
			cmd := exec.Command(p, "--app="+url, "--user-data-dir="+profileDir, "--window-size=1180,820", "--no-first-run")
			if cmd.Start() == nil {
				go cmd.Wait()
				return
			}
		}
	}
	exec.Command("xdg-open", url).Start()
}

func openFolder(path string) { exec.Command("xdg-open", path).Start() }

func ensureFirewall(javaPath string, port int) string { return "" }
