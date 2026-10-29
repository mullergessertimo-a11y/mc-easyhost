//go:build linux

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseUfwEnabled(t *testing.T) {
	for content, want := range map[string]bool{
		"# comment\nENABLED=yes\nLOGLEVEL=low\n": true,
		"ENABLED=no\n":                           false,
		"ENABLED=\"YES\"\n":                      true,
		"LOGLEVEL=low\n":                         false,
	} {
		p := filepath.Join(t.TempDir(), "ufw.conf")
		os.WriteFile(p, []byte(content), 0o644)
		f, _ := os.Open(p)
		if got := parseUfwEnabled(f); got != want {
			t.Errorf("parseUfwEnabled(%q)=%v want %v", content, got, want)
		}
		f.Close()
	}
}
