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

// ensureFirewall opens the server port (TCP) and mod ports (UDP, e.g. voice
// chat) in ufw or firewalld when one of them is switched on. Most desktop
// distros ship with neither active, in which case there is nothing to do.
// Changing the firewall needs root, so the desktop shows one password
// prompt through pkexec for all missing ports together.
func ensureFirewall(dataDir, javaPath string, port int, udp []int) string {
	specs := []string{fmt.Sprintf("%d/tcp", port)}
	for _, u := range udp {
		specs = append(specs, fmt.Sprintf("%d/udp", u))
	}
	switch {
	case ufwEnabled():
		// ufw rules can't be read without root, so remember what we added.
		marker := func(spec string) string {
			return filepath.Join(dataDir, "ufw-"+strings.ReplaceAll(spec, "/", "-")+".done")
		}
		var missing, cmds []string
		for _, spec := range specs {
			if _, err := os.Stat(marker(spec)); err != nil {
				missing = append(missing, spec)
				cmds = append(cmds, "ufw allow "+spec+" comment 'MC EasyHost'")
			}
		}
		if len(missing) == 0 {
			return ""
		}
		if err := runAsRoot("sh", "-c", strings.Join(cmds, " && ")); err != nil {
			return "The ufw firewall was not changed (" + err.Error() + "). If friends cannot connect, run: sudo " + strings.Join(cmds, " && sudo ")
		}
		for _, spec := range missing {
			os.WriteFile(marker(spec), nil, 0o644)
		}
		return "Allowed " + strings.Join(missing, ", ") + " through the ufw firewall."

	case firewalldRunning():
		var missing, cmds []string
		for _, spec := range specs {
			if exec.Command("firewall-cmd", "--query-port="+spec).Run() != nil {
				missing = append(missing, spec)
				cmds = append(cmds, "firewall-cmd --permanent --add-port="+spec, "firewall-cmd --add-port="+spec)
			}
		}
		if len(missing) == 0 {
			return ""
		}
		if err := runAsRoot("sh", "-c", strings.Join(cmds, " && ")); err != nil {
			return "firewalld was not changed (" + err.Error() + "). If friends cannot connect, run: sudo " + strings.Join(cmds, " && sudo ")
		}
		return "Allowed " + strings.Join(missing, ", ") + " through firewalld."
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
