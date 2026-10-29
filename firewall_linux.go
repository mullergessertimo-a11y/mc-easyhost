//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ensureFirewall opens the server port in ufw or firewalld when one of them
// is switched on. Most desktop distros ship with neither active, in which
// case there is nothing to do. Changing the firewall needs root, so the
// desktop shows one password prompt through pkexec.
func ensureFirewall(dataDir, javaPath string, port int) string {
	switch {
	case ufwEnabled():
		// ufw rules can't be read without root, so remember what we added.
		marker := filepath.Join(dataDir, fmt.Sprintf("ufw-%d.done", port))
		if _, err := os.Stat(marker); err == nil {
			return ""
		}
		manual := fmt.Sprintf("sudo ufw allow %d/tcp", port)
		if err := runAsRoot("ufw", "allow", fmt.Sprintf("%d/tcp", port), "comment", "MC EasyHost"); err != nil {
			return "The ufw firewall was not changed (" + err.Error() + "). If friends cannot connect, run: " + manual
		}
		os.WriteFile(marker, nil, 0o644)
		return fmt.Sprintf("Allowed port %d through the ufw firewall.", port)

	case firewalldRunning():
		p := fmt.Sprintf("%d/tcp", port)
		if exec.Command("firewall-cmd", "--query-port="+p).Run() == nil {
			return ""
		}
		manual := fmt.Sprintf("sudo firewall-cmd --permanent --add-port=%s && sudo firewall-cmd --reload", p)
		if err := runAsRoot("sh", "-c", "firewall-cmd --permanent --add-port="+p+" && firewall-cmd --add-port="+p); err != nil {
			return "firewalld was not changed (" + err.Error() + "). If friends cannot connect, run: " + manual
		}
		return fmt.Sprintf("Allowed port %d through firewalld.", port)
	}
	return ""
}

// ufwEnabled reads ufw's own config, which unlike `ufw status` needs no root.
func ufwEnabled() bool {
	if _, err := exec.LookPath("ufw"); err != nil {
		return false
	}
	f, err := os.Open("/etc/ufw/ufw.conf")
	if err != nil {
		return false
	}
	defer f.Close()
	return parseUfwEnabled(f)
}

func parseUfwEnabled(f *os.File) bool {
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "ENABLED="); ok {
			return strings.EqualFold(strings.Trim(v, `"' `), "yes")
		}
	}
	return false
}

func firewalldRunning() bool {
	if _, err := exec.LookPath("firewall-cmd"); err != nil {
		return false
	}
	return exec.Command("firewall-cmd", "--state").Run() == nil
}

// runAsRoot uses pkexec, which shows the desktop's own password dialog.
// Without a desktop session (e.g. over SSH) it fails and we print the
// command for the user to run instead.
func runAsRoot(name string, args ...string) error {
	if os.Geteuid() == 0 {
		return exec.Command(name, args...).Run()
	}
	if _, err := exec.LookPath("pkexec"); err != nil {
		return fmt.Errorf("pkexec is not installed")
	}
	if err := exec.Command("pkexec", append([]string{name}, args...)...).Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && (ee.ExitCode() == 126 || ee.ExitCode() == 127) {
			return fmt.Errorf("permission declined")
		}
		return err
	}
	return nil
}
