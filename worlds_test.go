package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// nbtWriter builds a level.dat like Minecraft writes it, for tests.
type nbtWriter struct{ b bytes.Buffer }

func (w *nbtWriter) name(tag byte, n string) {
	w.b.WriteByte(tag)
	binary.Write(&w.b, binary.BigEndian, uint16(len(n)))
	w.b.WriteString(n)
}
func (w *nbtWriter) compound(n string) { w.name(10, n) }
func (w *nbtWriter) end()              { w.b.WriteByte(0) }
func (w *nbtWriter) str(n, v string) {
	w.name(8, n)
	binary.Write(&w.b, binary.BigEndian, uint16(len(v)))
	w.b.WriteString(v)
}
func (w *nbtWriter) i32(n string, v int32) { w.name(3, n); binary.Write(&w.b, binary.BigEndian, v) }
func (w *nbtWriter) i64(n string, v int64) { w.name(4, n); binary.Write(&w.b, binary.BigEndian, v) }
func (w *nbtWriter) i8(n string, v int8)   { w.name(1, n); w.b.WriteByte(byte(v)) }

func writeTestWorld(t *testing.T, dir, name, version string, seed int64) {
	t.Helper()
	var w nbtWriter
	w.compound("")
	w.compound("Data")
	w.str("LevelName", name)
	w.i64("LastPlayed", 1761800000000)
	w.i32("GameType", 1)
	w.i8("hardcore", 0)
	w.compound("Version")
	w.str("Name", version)
	w.i32("Id", 4440)
	w.end()
	w.compound("WorldGenSettings")
	w.i64("seed", seed)
	w.name(9, "dimensions_list") // a list, to exercise list parsing
	w.b.WriteByte(8)
	binary.Write(&w.b, binary.BigEndian, int32(2))
	for _, s := range []string{"overworld", "the_nether"} {
		binary.Write(&w.b, binary.BigEndian, uint16(len(s)))
		w.b.WriteString(s)
	}
	w.name(12, "longs") // long array, skipped by the reader
	binary.Write(&w.b, binary.BigEndian, int32(2))
	binary.Write(&w.b, binary.BigEndian, int64(1))
	binary.Write(&w.b, binary.BigEndian, int64(2))
	w.end()
	w.end()
	w.end()

	os.MkdirAll(filepath.Join(dir, "region"), 0o755)
	f, err := os.Create(filepath.Join(dir, "level.dat"))
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	gz.Write(w.b.Bytes())
	gz.Close()
	f.Close()
	os.WriteFile(filepath.Join(dir, "region", "r.0.0.mca"), []byte("chunks"), 0o644)
	os.WriteFile(filepath.Join(dir, "session.lock"), []byte("lock"), 0o644)
}

func TestReadWorldInfo(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My World")
	writeTestWorld(t, dir, "Craft Island", "1.21.4", -4172144997902289642)
	w, err := readWorldInfo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if w.Name != "Craft Island" || w.Version != "1.21.4" || w.Seed != "-4172144997902289642" || w.GameMode != "creative" || w.Folder != "My World" {
		t.Fatalf("got %+v", w)
	}
}

// fakeHome points the launcher lookups at a temp folder.
func fakeHome(t *testing.T) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	return home
}

func launcherSaves(home string) (official, prism string) {
	if runtime.GOOS == "windows" {
		a := filepath.Join(home, "AppData", "Roaming")
		return filepath.Join(a, ".minecraft", "saves"), filepath.Join(a, "PrismLauncher", "instances", "Craft Pack", ".minecraft", "saves")
	}
	return filepath.Join(home, ".minecraft", "saves"),
		filepath.Join(home, ".local", "share", "PrismLauncher", "instances", "Craft Pack", "minecraft", "saves")
}

func testManager(t *testing.T) *Manager {
	data := t.TempDir()
	cfg := loadConfig(data)
	c := cfg.Get()
	c.Version = "26.3" // avoid a network lookup for "latest"
	cfg.Set(c)
	return NewManager(data, cfg, NewHub())
}

func TestSingleplayerDiscoveryAndImport(t *testing.T) {
	home := fakeHome(t)
	official, prism := launcherSaves(home)
	writeTestWorld(t, filepath.Join(official, "New World"), "Survival", "1.21.4", 42)
	writeTestWorld(t, filepath.Join(prism, "modded"), "Modded", "26.3", 7)
	os.MkdirAll(filepath.Join(official, "not a world"), 0o755)

	worlds := SingleplayerWorlds()
	if len(worlds) != 2 {
		t.Fatalf("found %d worlds: %+v", len(worlds), worlds)
	}
	var sources []string
	for _, w := range worlds {
		sources = append(sources, w.Source)
	}
	if !strings.Contains(strings.Join(sources, "|"), "Prism: Craft Pack") || !strings.Contains(strings.Join(sources, "|"), "Minecraft Launcher") {
		t.Errorf("sources: %v", sources)
	}

	m := testManager(t)
	// An existing server world must be backed up before it is replaced.
	writeTestWorld(t, filepath.Join(m.serverDir, "world"), "Old Server World", "26.3", 1)
	os.MkdirAll(filepath.Join(m.serverDir, "world_nether", "DIM-1"), 0o755)

	if err := m.ImportWorld(filepath.Join(official, "New World")); err != nil {
		t.Fatal(err)
	}
	cur := m.CurrentWorld()
	if cur == nil || cur.Name != "Survival" || cur.Seed != "42" {
		t.Fatalf("current world after import: %+v", cur)
	}
	if _, err := os.Stat(filepath.Join(m.serverDir, "world", "session.lock")); err == nil {
		t.Error("session.lock was copied")
	}
	if _, err := os.Stat(filepath.Join(m.serverDir, "world_nether")); err == nil {
		t.Error("old nether folder was left behind")
	}
	if _, err := os.Stat(filepath.Join(official, "New World", "level.dat")); err != nil {
		t.Error("original singleplayer world was touched")
	}
	if b := m.ListBackups(); len(b) != 1 {
		t.Fatalf("expected a backup of the old world, got %+v", b)
	}

	// Paths that aren't a discovered world are refused.
	if err := m.ImportWorld(home); err == nil {
		t.Error("imported an arbitrary folder")
	}
}

func TestImportRefusesNewerWorld(t *testing.T) {
	home := fakeHome(t)
	official, _ := launcherSaves(home)
	writeTestWorld(t, filepath.Join(official, "Future"), "Future", "27.1", 1)
	m := testManager(t)
	err := m.ImportWorld(filepath.Join(official, "Future"))
	if err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("expected a version error, got %v", err)
	}
}

func TestNewWorldAndRestore(t *testing.T) {
	fakeHome(t)
	m := testManager(t)
	writeTestWorld(t, filepath.Join(m.serverDir, "world"), "Keep Me", "26.3", 99)

	if err := m.NewWorld("craftattack", "large_biomes"); err != nil {
		t.Fatal(err)
	}
	if worldExists(m.serverDir) {
		t.Fatal("old world still there")
	}
	c := m.cfg.Get()
	if c.WorldSeed != "craftattack" || c.WorldType != "large_biomes" {
		t.Fatalf("config: %+v", c)
	}
	props := filepath.Join(m.serverDir, "server.properties")
	writeServerProperties(props, c, "26.3")
	b, _ := os.ReadFile(props)
	for _, want := range []string{"level-seed=craftattack", "level-type=minecraft:large_biomes", "level-name=world"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %q", want)
		}
	}
	writeServerProperties(props, c, "1.18.2")
	b, _ = os.ReadFile(props)
	if !strings.Contains(string(b), "level-type=largeBiomes") {
		t.Errorf("old versions need legacy level-type:\n%s", b)
	}

	backups := m.ListBackups()
	if len(backups) != 1 {
		t.Fatalf("backups: %+v", backups)
	}
	if err := m.RestoreBackup(backups[0].Name); err != nil {
		t.Fatal(err)
	}
	if w := m.CurrentWorld(); w == nil || w.Name != "Keep Me" {
		t.Fatalf("restore gave %+v", w)
	}
	if err := m.RestoreBackup("../../etc/passwd"); err == nil {
		t.Error("accepted a path as backup name")
	}
	if err := m.NewWorld("x", "bogus"); err == nil {
		t.Error("accepted unknown world type")
	}
}

func TestImportZip(t *testing.T) {
	fakeHome(t)
	src := filepath.Join(t.TempDir(), "Friends World")
	writeTestWorld(t, src, "From A Friend", "1.21.10", 5)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(filepath.Dir(src), p)
		w, _ := zw.Create(filepath.ToSlash(rel)) // zipped with its folder, like Windows does
		b, _ := os.ReadFile(p)
		w.Write(b)
		return nil
	})
	zw.Close()

	m := testManager(t)
	if err := m.ImportWorldZip(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatal(err)
	}
	if w := m.CurrentWorld(); w == nil || w.Name != "From A Friend" {
		t.Fatalf("got %+v", w)
	}
	if err := m.ImportWorldZip(strings.NewReader("not a zip")); err == nil {
		t.Error("accepted garbage")
	}
}

func TestWorldChangesNeedStoppedServer(t *testing.T) {
	m := testManager(t)
	m.status.State = StateRunning
	if err := m.NewWorld("", "normal"); err == nil {
		t.Error("NewWorld while running")
	}
	if err := m.RemoveMod("x.jar"); err == nil {
		t.Error("RemoveMod while running")
	}
}
