//go:build darwin

package main

import "os/exec"

func setParentDeathSignal(cmd *exec.Cmd) {}
