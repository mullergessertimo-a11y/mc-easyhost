package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Online: EASYHOST_ONLINE=1 go test -run OnlineMods -v
func TestOnlineModsPaperAndFabric(t *testing.T) {
	if os.Getenv("EASYHOST_ONLINE") == "" {
		t.Skip("set EASYHOST_ONLINE=1")
	}
	m := testManager(t)

	// Paper: plugins, voice chat opens a UDP port.
	hits, total, err := m.SearchMods("voice chat", 0)
	if err != nil || total == 0 {
		t.Fatalf("search: %v %d", err, total)
	}
	var voice string
	for _, h := range hits {
		if h.Slug == "simple-voice-chat" {
			voice = h.ID
		}
	}
	if voice == "" {
		t.Fatal("Simple Voice Chat not found")
	}
	got, err := m.InstallMod(voice)
	if err != nil || len(got) != 1 {
		t.Fatalf("install voice chat: %v %v", got, err)
	}
	list, folder, _ := m.ListMods()
	if folder != "plugins" || len(list) != 1 || list[0].Slug != "simple-voice-chat" {
		t.Fatalf("list: %s %+v", folder, list)
	}
	if ports := m.extraUDPPorts("paper", 25565); len(ports) != 1 || ports[0] != 24454 {
		t.Errorf("voice chat UDP port: %v", ports)
	}
	os.WriteFile(filepath.Join(m.serverDir, "plugins", "handmade.jar"), []byte("x"), 0o644)
	list, _, _ = m.ListMods()
	if len(list) != 2 {
		t.Errorf("hand-added jar not listed: %+v", list)
	}
	for _, im := range list {
		if im.Slug == "simple-voice-chat" {
			if err := m.RemoveMod(im.File); err != nil {
				t.Fatal(err)
			}
		}
	}
	if list, _, _ = m.ListMods(); len(list) != 1 || !list[0].Unknown {
		t.Errorf("after removing voice chat: %+v", list)
	}
	if err := m.RemoveMod("../config.json"); err == nil {
		t.Error("removed a path outside the folder")
	}

	// Fabric: mods, required dependencies come along.
	c := m.cfg.Get()
	c.ServerType = "fabric"
	m.cfg.Set(c)
	got, err = m.InstallMod("chunky")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("installed for fabric: %v", got)
	list, folder, _ = m.ListMods()
	slugs := map[string]bool{}
	for _, im := range list {
		slugs[im.Slug] = true
		if im.Slug == "fabric-api" && !im.Dependency {
			t.Error("Fabric API should be marked as a dependency")
		}
	}
	if folder != "mods" || !slugs["chunky"] || !slugs["fabric-api"] {
		t.Fatalf("fabric mods: %s %+v", folder, list)
	}
	// The paper plugin folder is untouched by the fabric switch.
	if _, err := os.Stat(filepath.Join(m.serverDir, "plugins", "handmade.jar")); err != nil {
		t.Error("plugins folder changed")
	}

	// Installing again is a no-op; update keeps everything.
	if got, err := m.InstallMod("chunky"); err != nil || len(got) != 0 {
		t.Errorf("reinstall: %v %v", got, err)
	}
	if updated, problems := m.UpdateMods(); len(problems) != 0 {
		t.Errorf("update: %v %v", updated, problems)
	}

	// Vanilla can't have mods.
	c.ServerType = "vanilla"
	m.cfg.Set(c)
	if _, _, err := m.SearchMods("", 0); err == nil || !strings.Contains(err.Error(), "Paper") {
		t.Errorf("vanilla search: %v", err)
	}
}

func TestOnlineFabricBuild(t *testing.T) {
	if os.Getenv("EASYHOST_ONLINE") == "" {
		t.Skip("set EASYHOST_ONLINE=1")
	}
	b, err := resolveBuild("fabric", "latest")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("fabric latest: %s java %d %s", b.Version, b.JavaMajor, b.JarName)
	jar := filepath.Join(t.TempDir(), b.JarName)
	if err := downloadFile(b.URL, jar, b.HashAlgo, b.Hash, nil); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(jar); st.Size() < 50_000 {
		t.Fatalf("fabric launcher jar is only %d bytes", st.Size())
	}
}
