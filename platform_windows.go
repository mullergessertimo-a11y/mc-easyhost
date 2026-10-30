//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

var jobHandle windows.Handle

// initPlatform creates a job object so Java dies with us even if EasyHost
// is killed from Task Manager (otherwise the world stays locked).
func initPlatform() {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(h)
		return
	}
	jobHandle = h
}

func setupChildProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

func attachChildProcess(cmd *exec.Cmd) {
	if jobHandle == 0 {
		return
	}
	p, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return
	}
	windows.AssignProcessToJobObject(jobHandle, p)
	windows.CloseHandle(p)
}

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

func totalMemoryMB() int {
	var ms memoryStatusEx
	ms.Length = uint32(unsafe.Sizeof(ms))
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(&ms))); r == 0 {
		return 0
	}
	return int(ms.TotalPhys / 1024 / 1024)
}

// openAppWindow shows the UI in a chromeless Edge/Chrome window so it looks
// like a normal desktop app. Edge ships with every Windows 10/11.
func openAppWindow(url, profileDir string) {
	var candidates []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			cmd := exec.Command(c, "--app="+url, "--user-data-dir="+profileDir,
				"--window-size=1180,820", "--no-first-run", "--no-default-browser-check",
				"--disable-features=Translate")
			if cmd.Start() == nil {
				go cmd.Wait()
				return
			}
		}
	}
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

func openFolder(path string) {
	exec.Command("explorer.exe", path).Start()
}

// ensureFirewall adds inbound Windows Firewall rules for the server port
// (TCP) and any mod ports (UDP, e.g. voice chat), each only once. Changing
// the firewall needs admin rights, so Windows shows one UAC prompt for all
// missing rules together.
func ensureFirewall(dataDir, javaPath string, port int, udp []int) string {
	type rule struct {
		proto string
		port  int
	}
	want := []rule{{"TCP", port}}
	for _, u := range udp {
		want = append(want, rule{"UDP", u})
	}
	var missing []string
	var added []string
	for _, r := range want {
		name := fmt.Sprintf("MC EasyHost (%s %d)", r.proto, r.port)
		check := exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name="+name)
		check.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
		if check.Run() == nil {
			continue
		}
		missing = append(missing, fmt.Sprintf(`netsh advfirewall firewall add rule name="%s" dir=in action=allow protocol=%s localport=%d program="%s" profile=any`,
			name, r.proto, r.port, javaPath))
		added = append(added, fmt.Sprintf("%s %d", r.proto, r.port))
	}
	if len(missing) == 0 {
		return ""
	}
	// One elevated cmd runs every netsh call, so there is a single prompt.
	ps := fmt.Sprintf(`Start-Process -FilePath cmd -Verb RunAs -WindowStyle Hidden -Wait -ArgumentList '/c %s'`,
		strings.ReplaceAll(strings.Join(missing, " & "), "'", "''"))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := cmd.Run(); err != nil {
		return "Windows Firewall was not changed (permission declined). If friends cannot connect, allow Java when Windows asks."
	}
	return "Allowed " + strings.Join(added, ", ") + " through Windows Firewall."
}

// defaultDataDir is %APPDATA%\MC EasyHost.
func defaultDataDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "MC EasyHost")
	}
	return "MC EasyHost"
}
