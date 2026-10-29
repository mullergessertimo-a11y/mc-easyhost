//go:build darwin

package main

import "os/exec"

func setParentDeathSignal(cmd *exec.Cmd) {}

func ensureFirewall(dataDir, javaPath string, port int) string { return "" }
