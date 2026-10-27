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

// ensureFirewall adds an inbound Windows Firewall rule for the server once.
// Changing the firewall needs admin rights, so Windows shows one UAC prompt.
func ensureFirewall(javaPath string, port int) string {
	name := fmt.Sprintf("MC EasyHost (TCP %d)", port)
	check := exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name="+name)
	check.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if check.Run() == nil {
		return ""
	}
	args := fmt.Sprintf(`advfirewall firewall add rule name="%s" dir=in action=allow protocol=TCP localport=%d program="%s" profile=any`,
		name, port, javaPath)
	ps := fmt.Sprintf(`Start-Process -FilePath netsh -Verb RunAs -WindowStyle Hidden -Wait -ArgumentList '%s'`,
		strings.ReplaceAll(args, "'", "''"))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := cmd.Run(); err != nil {
		return "Windows Firewall was not changed (permission declined). If friends cannot connect, allow Java when Windows asks."
	}
	return fmt.Sprintf("Allowed port %d through Windows Firewall.", port)
}
