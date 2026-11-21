package main

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGeyserConfigPort(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yml")
	os.WriteFile(p, []byte("# Geyser\nbedrock:\n  # the port\n  port: 19133\n  motd1: x\njava:\n  port: 25565\n"), 0o644)
	if got := geyserConfigPort(p); got != 19133 {
		t.Fatalf("port %d", got)
	}
	os.WriteFile(p, []byte("java:\n  port: 25570\n"), 0o644)
	if got := geyserConfigPort(p); got != 0 {
		t.Fatalf("java port taken as bedrock: %d", got)
	}
}

func TestCrossplayPortsAndDetection(t *testing.T) {
	m := testManager(t)
	if m.bedrockPort("paper") != 0 {
		t.Fatal("crossplay reported without Geyser")
	}
	os.MkdirAll(filepath.Join(m.serverDir, "plugins"), 0o755)
	os.WriteFile(filepath.Join(m.serverDir, "plugins", "Geyser-Spigot.jar"), []byte("x"), 0o644)
	m.saveModsManifest(map[string]InstalledMod{"plugins/Geyser-Spigot.jar": {File: "Geyser-Spigot.jar", ProjectID: geyserProjectID + "geyser", Slug: "geyser"}})
	if p := m.bedrockPort("paper"); p != 19132 {
		t.Fatalf("bedrock port %d", p)
	}
	if p := m.bedrockPort("fabric"); p != 0 {
		t.Fatalf("paper plugin counted for fabric: %d", p)
	}
	if got := m.extraUDPPorts("paper", 25565); len(got) != 1 || got[0] != 19132 {
		t.Fatalf("udp ports %v", got)
	}
}

func TestPackPath(t *testing.T) {
	m := testManager(t)
	for _, bad := range []string{"../evil.jar", "/etc/passwd", "mods/../../x", "C:/Windows/x", `..\x`} {
		if _, err := m.packPath(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	for _, skip := range []string{"world/level.dat", "server.properties", "easyhost-mods.json", "ops.json"} {
		if p, err := m.packPath(skip); err != nil || p != "" {
			t.Errorf("%q not skipped: %q %v", skip, p, err)
		}
	}
	if p, err := m.packPath("mods/sodium.jar"); err != nil || p != filepath.Join(m.serverDir, "mods", "sodium.jar") {
		t.Errorf("mods path: %q %v", p, err)
	}
}

func writePack(t *testing.T, index map[string]any, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "pack.mrpack")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("modrinth.index.json")
	json.NewEncoder(w).Encode(index)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
	return p
}

func TestInstallMrpackOffline(t *testing.T) {
	m := testManager(t)
	os.MkdirAll(filepath.Join(m.serverDir, "mods"), 0o755)
	os.WriteFile(filepath.Join(m.serverDir, "mods", "old.jar"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(m.serverDir, "server.properties"), []byte("motd=mine\n"), 0o644)
	pack := writePack(t, map[string]any{
		"formatVersion": 1, "game": "minecraft", "name": "Test Pack", "versionId": "1.0",
		"files":        []any{map[string]any{"path": "mods/client-only.jar", "env": map[string]string{"server": "unsupported"}, "hashes": map[string]string{"sha1": "x"}, "downloads": []string{"https://cdn.modrinth.com/x"}}},
		"dependencies": map[string]string{"minecraft": "1.21.1", "fabric-loader": "0.16.5"},
	}, map[string]string{
		"overrides/config/a.json":        "from overrides",
		"overrides/config/b.json":        "keep",
		"server-overrides/config/a.json": "from server-overrides",
		"overrides/world/level.dat":      "must not land",
		"overrides/server.properties":    "motd=pack\n",
	})
	p, err := m.installMrpack(pack, Modpack{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "Test Pack" || p.Minecraft != "1.21.1" || p.Loader != "0.16.5" {
		t.Fatalf("pack info %+v", p)
	}
	c := m.cfg.Get()
	if c.ServerType != "fabric" || c.Version != "1.21.1" || c.FabricLoader != "0.16.5" {
		t.Fatalf("config %+v", c)
	}
	read := func(rel string) string { b, _ := os.ReadFile(filepath.Join(m.serverDir, rel)); return string(b) }
	if read("config/a.json") != "from server-overrides" || read("config/b.json") != "keep" {
		t.Error("overrides not applied in order")
	}
	if _, err := os.Stat(filepath.Join(m.serverDir, "world")); err == nil {
		t.Error("pack wrote into the world")
	}
	if read("server.properties") != "motd=mine\n" {
		t.Error("pack overwrote server.properties")
	}
	if _, err := os.Stat(filepath.Join(m.serverDir, "mods", "old.jar")); err == nil {
		t.Error("old mods not moved aside")
	}
	if cur := m.CurrentModpack(); cur == nil || cur.Name != "Test Pack" {
		t.Errorf("current modpack %+v", cur)
	}
	// Switching version in settings drops the pack.
	c.Version = "1.21.4"
	m.cfg.Set(c)
	if m.CurrentModpack() != nil {
		t.Error("pack still shown after a version change")
	}
}

func TestMrpackRejects(t *testing.T) {
	m := testManager(t)
	forge := writePack(t, map[string]any{"formatVersion": 1, "game": "minecraft", "name": "F",
		"dependencies": map[string]string{"minecraft": "1.20.1", "forge": "47.2.0"}}, nil)
	if _, err := m.installMrpack(forge, Modpack{}); err == nil || !strings.Contains(err.Error(), "Forge") {
		t.Errorf("forge pack: %v", err)
	}
	badHost := writePack(t, map[string]any{"formatVersion": 1, "game": "minecraft", "name": "B",
		"files":        []any{map[string]any{"path": "mods/x.jar", "hashes": map[string]string{"sha1": "ab"}, "downloads": []string{"https://evil.example/x.jar"}}},
		"dependencies": map[string]string{"minecraft": "1.21.1", "fabric-loader": "0.16.5"}}, nil)
	if _, err := m.installMrpack(badHost, Modpack{}); err == nil {
		t.Error("accepted a download from a random site")
	}
	if c := m.cfg.Get(); c.ServerType == "fabric" {
		t.Error("config changed by a rejected pack")
	}
}

func TestPrettyJarNameAndVersions(t *testing.T) {
	for in, want := range map[string]string{
		"sodium-fabric-0.5.8+mc1.20.4.jar": "sodium",
		"lithium-fabric-mc1.21.1-0.13.jar": "lithium",
		"fabric-api-0.100.0+1.21.jar":      "fabric api",
		"Chunky-1.4.10.jar":                "Chunky",
	} {
		if got := prettyJarName(in); got != want {
			t.Errorf("%s: %q", in, got)
		}
	}
	if !isNewer("v1.4.0", "v1.3.0") || isNewer("v1.3.0", "v1.3.0") || isNewer("v1.2.9", "v1.3.0") || !isNewer("v1.10.0", "v1.9.2") {
		t.Error("isNewer")
	}
}

// fakePlayit emulates the parts of the playit.gg API we use.
type fakePlayit struct {
	mu      sync.Mutex
	tunnels []map[string]any
	created []map[string]any
	auth    []string
}

func (f *fakePlayit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	var req map[string]any
	json.NewDecoder(r.Body).Decode(&req)
	ok := func(data any) { json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data}) }
	switch r.URL.Path {
	case "/agents/rundata":
		ok(map[string]any{"agent_id": "a-1", "account_status": "ready", "tunnels": f.tunnels, "pending": []any{}})
	case "/tunnels/create":
		f.created = append(f.created, req)
		origin := req["origin"].(map[string]any)["data"].(map[string]any)
		domain, port := "cool-name.gl.joinmc.link", 41234
		if req["tunnel_type"] == "minecraft-bedrock" {
			domain, port = "bedrock-name.gl.at.ply.gg", 5555
		}
		f.tunnels = append(f.tunnels, map[string]any{"id": "t" + domain, "tunnel_type": req["tunnel_type"],
			"local_port": origin["local_port"], "assigned_domain": domain, "port": map[string]int{"from": port, "to": port + 1}})
		ok(map[string]string{"id": "t1"})
	case "/claim/setup":
		json.NewEncoder(w).Encode(map[string]any{"status": "fail", "data": "CodeNotFound"})
	default:
		json.NewEncoder(w).Encode(map[string]any{"status": "error", "data": map[string]string{"type": "x", "message": "not here"}})
	}
}

func TestEnsureTunnelsWithFakePlayit(t *testing.T) {
	f := &fakePlayit{}
	srv := httptest.NewServer(f)
	defer srv.Close()
	old := playitAPI
	playitAPI = srv.URL
	defer func() { playitAPI = old }()

	java, bedrock, err := ensureTunnels("abc123", 25570, 19132, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if java != "cool-name.gl.joinmc.link" || bedrock != "bedrock-name.gl.at.ply.gg:5555" {
		t.Fatalf("addresses %q %q", java, bedrock)
	}
	if len(f.created) != 2 || f.created[0]["tunnel_type"] != "minecraft-java" || f.created[0]["port_type"] != "tcp" ||
		f.created[1]["port_type"] != "udp" {
		t.Fatalf("created %+v", f.created)
	}
	if f.auth[0] != "Agent-Key abc123" {
		t.Fatalf("auth header %q", f.auth[0])
	}
	// Second run reuses the tunnels instead of making new ones.
	if _, _, err := ensureTunnels("abc123", 25570, 19132, time.Second); err != nil || len(f.created) != 2 {
		t.Fatalf("recreated tunnels: %v %d", err, len(f.created))
	}

	var state string
	err = playitCall("/claim/setup", "", map[string]string{"code": "x"}, &state)
	if pf, ok := err.(*playitFail); !ok || pf.Code != "CodeNotFound" {
		t.Fatalf("fail decoding: %v", err)
	}
	if err := playitCall("/nope", "", nil, nil); err == nil || !strings.Contains(err.Error(), "not here") {
		t.Fatalf("error decoding: %v", err)
	}
}

func TestOnlineGeyserAndModpackSearch(t *testing.T) {
	if os.Getenv("EASYHOST_ONLINE") == "" {
		t.Skip("set EASYHOST_ONLINE=1")
	}
	m := testManager(t)
	mf := map[string]InstalledMod{}
	name, err := m.installGeyserPlugin(mf, "floodgate")
	if err != nil || name != "Floodgate" {
		t.Fatalf("floodgate: %q %v", name, err)
	}
	if len(mf) != 1 {
		t.Fatalf("manifest %+v", mf)
	}
	hits, total, err := m.SearchModpacks("", 0)
	if err != nil || total == 0 || len(hits) == 0 {
		t.Fatalf("modpack search: %v %d", err, total)
	}
	t.Logf("top modpack: %s (%d results)", hits[0].Title, total)

	u := &Updater{}
	old := version
	version = "v0.0.1"
	defer func() { version = old }()
	info := u.Check(true)
	if !info.Available || info.Latest == "" {
		t.Fatalf("update check: %+v", info)
	}
	t.Logf("latest release %s, canInstall=%v", info.Latest, info.CanInstall)
}
func TestOnlinePinnedFabricLoader(t *testing.T) {
	if os.Getenv("EASYHOST_ONLINE") == "" {
		t.Skip("set EASYHOST_ONLINE=1")
	}
	b, err := resolveBuild("fabric", "26.2", "0.20.1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.URL, "/26.2/0.20.1/") || !strings.Contains(b.JarName, "loader.0.20.1") {
		t.Fatalf("loader not pinned: %s %s", b.URL, b.JarName)
	}
	resp, err := http.Head(b.URL)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("HEAD %s: %v %v", b.URL, err, resp)
	}
}
