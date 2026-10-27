//go:build linux

package main

import (
	"os/exec"
	"syscall"
)

// Java saves the world on SIGTERM, so ask it to shut down if we die.
func setParentDeathSignal(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
