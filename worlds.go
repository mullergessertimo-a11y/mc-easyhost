package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// worldDirs are the folders a server keeps its world in. Vanilla and
// Fabric use world/ only; Paper splits the nether and the end out.
var worldDirs = []string{"world", "world_nether", "world_the_end"}

type WorldInfo struct {
	Path       string `json:"path,omitempty"`
	Folder     string `json:"folder"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Seed       string `json:"seed"` // string: JavaScript can't hold a 64-bit number exactly
	LastPlayed int64  `json:"lastPlayed"`
	GameMode   string `json:"gameMode"`
	Hardcore   bool   `json:"hardcore"`
	Source     string `json:"source,omitempty"` // launcher it was found in
	SizeMB     int64  `json:"sizeMB"`
}

type BackupInfo struct {
	Name    string `json:"name"`
	SizeMB  int64  `json:"sizeMB"`
	Created int64  `json:"created"`
}

func readWorldInfo(dir string) (*WorldInfo, error) {
	root, err := readLevelDat(filepath.Join(dir, "level.dat"))
	if err != nil {
		return nil, err
	}
	w := &WorldInfo{Folder: filepath.Base(dir)}
	if s, ok := nbtPath(root, "Data", "LevelName").(string); ok {
		w.Name = s
	}
	if s, ok := nbtPath(root, "Data", "Version", "Name").(string); ok {
		w.Version = s
	}
	for _, p := range [][]string{{"Data", "WorldGenSettings", "seed"}, {"Data", "RandomSeed"}} {
		if s, ok := nbtPath(root, p...).(int64); ok {
			w.Seed = strconv.FormatInt(s, 10)
			break
		}
	}
	if t, ok := nbtPath(root, "Data", "LastPlayed").(int64); ok {
		w.LastPlayed = t
	}
	if g, ok := nbtPath(root, "Data", "GameType").(int32); ok {
		w.GameMode = map[int32]string{0: "survival", 1: "creative", 2: "adventure", 3: "spectator"}[g]
	}
	if h, ok := nbtPath(root, "Data", "hardcore").(int8); ok {
		w.Hardcore = h != 0
	}
	if w.Name == "" {
		w.Name = w.Folder
	}
	return w, nil
}

func dirSizeMB(dirs ...string) int64 {
	var total int64
	for _, d := range dirs {
		filepath.Walk(d, func(_ string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				total += info.Size()
			}
			return nil
		})
	}
	return total / (1 << 20)
}

// CurrentWorld describes the world the server will load, or nil.
func (m *Manager) CurrentWorld() *WorldInfo {
	w, err := readWorldInfo(filepath.Join(m.serverDir, "world"))
	if err != nil {
		return nil
	}
	var dirs []string
	for _, d := range worldDirs {
		dirs = append(dirs, filepath.Join(m.serverDir, d))
	}
	w.SizeMB = dirSizeMB(dirs...)
	return w
}

// worldVersion is the Minecraft version the current world was last saved
// with, read from the world itself.
func worldVersion(serverDir string) string {
	if w, err := readWorldInfo(filepath.Join(serverDir, "world")); err == nil {
		return w.Version
	}
	return ""
}

// saveRoots lists the "saves" folders of the common launchers.
func saveRoots() []struct{ Dir, Source string } {
	home, _ := os.UserHomeDir()
	type pattern struct{ glob, source string }
	var pats []pattern
	instance := func(base, label string, sub ...string) {
		for _, s := range sub {
			pats = append(pats, pattern{filepath.Join(append([]string{base, "*"}, append(strings.Split(s, "/"), "saves")...)...), label})
		}
	}
	if runtime.GOOS == "windows" {
		appdata := os.Getenv("APPDATA")
		pats = append(pats, pattern{filepath.Join(appdata, ".minecraft", "saves"), "Minecraft Launcher"})
		instance(filepath.Join(appdata, "PrismLauncher", "instances"), "Prism", ".minecraft", "minecraft")
		instance(filepath.Join(appdata, "ModrinthApp", "profiles"), "Modrinth", "")
		instance(filepath.Join(appdata, "com.modrinth.theseus", "profiles"), "Modrinth", "")
		instance(filepath.Join(home, "curseforge", "minecraft", "Instances"), "CurseForge", "")
	} else {
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			data = filepath.Join(home, ".local", "share")
		}
		pats = append(pats, pattern{filepath.Join(home, ".minecraft", "saves"), "Minecraft Launcher"},
			pattern{filepath.Join(home, ".var", "app", "com.mojang.Minecraft", ".minecraft", "saves"), "Minecraft Launcher"},
			pattern{filepath.Join(home, "Library", "Application Support", "minecraft", "saves"), "Minecraft Launcher"})
		instance(filepath.Join(data, "PrismLauncher", "instances"), "Prism", ".minecraft", "minecraft")
		instance(filepath.Join(home, ".var", "app", "org.prismlauncher.PrismLauncher", "data", "PrismLauncher", "instances"), "Prism", ".minecraft", "minecraft")
		instance(filepath.Join(data, "ModrinthApp", "profiles"), "Modrinth", "")
		instance(filepath.Join(data, "com.modrinth.theseus", "profiles"), "Modrinth", "")
	}
	var out []struct{ Dir, Source string }
	seen := map[string]bool{}
	for _, p := range pats {
		matches, _ := filepath.Glob(p.glob)
		for _, dir := range matches {
			if seen[dir] {
				continue
			}
			seen[dir] = true
			src := p.source
			if prefix, _, ok := strings.Cut(p.glob, "*"); ok { // name the instance/profile
				instanceName, _, _ := strings.Cut(filepath.ToSlash(strings.TrimPrefix(dir, prefix)), "/")
				src += ": " + instanceName
			}
			out = append(out, struct{ Dir, Source string }{dir, src})
		}
	}
	return out
}

// SingleplayerWorlds finds worlds in the installed launchers, newest first.
func SingleplayerWorlds() []WorldInfo {
	var out []WorldInfo
	for _, root := range saveRoots() {
		entries, _ := os.ReadDir(root.Dir)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(root.Dir, e.Name())
			w, err := readWorldInfo(dir)
			if err != nil {
				continue
			}
			w.Path, w.Source = dir, root.Source
			out = append(out, *w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastPlayed > out[j].LastPlayed })
	return out
}

// clearWorld backs the current world up and removes it, so a new or
// imported one can take its place.
func (m *Manager) clearWorld() error {
	if worldExists(m.serverDir) {
		if _, err := m.Backup(); err != nil {
			return fmt.Errorf("could not back up the current world first: %w", err)
		}
	}
	for _, d := range worldDirs {
		if err := os.RemoveAll(filepath.Join(m.serverDir, d)); err != nil {
			return fmt.Errorf("could not remove the old world (is it open somewhere?): %w", err)
		}
	}
	return nil
}

// ImportWorld copies a singleplayer world (found by SingleplayerWorlds) onto
// the server. The original stays untouched.
func (m *Manager) ImportWorld(path string) error {
	ok := false
	for _, w := range SingleplayerWorlds() {
		if w.Path == path {
			ok = true
		}
	}
	if !ok {
		return errors.New("that world was not found in any launcher folder")
	}
	return m.importWorldDir(path)
}

func (m *Manager) importWorldDir(src string) error {
	if !m.isStopped() {
		return errors.New("stop the server first")
	}
	w, err := readWorldInfo(src)
	if err != nil {
		return fmt.Errorf("this does not look like a Minecraft world (no readable level.dat): %w", err)
	}
	if target, err := m.targetVersion(); err == nil && w.Version != "" && compareVersions(w.Version, target) > 0 {
		return fmt.Errorf("%q was last played on Minecraft %s, newer than the server's %s. Set the server version to %s or newer in Settings first",
			w.Name, w.Version, target, w.Version)
	}
	if err := m.clearWorld(); err != nil {
		return err
	}
	m.log(fmt.Sprintf("[EasyHost] Copying world %q (%s)…", w.Name, w.Version))
	files, err := copyDir(src, filepath.Join(m.serverDir, "world"))
	if err != nil {
		os.RemoveAll(filepath.Join(m.serverDir, "world"))
		return fmt.Errorf("copying the world failed: %w", err)
	}
	m.log(fmt.Sprintf("[EasyHost] Imported %q (%d files). It loads the next time you press Start.", w.Name, files))
	return nil
}

// ImportWorldZip takes an uploaded zip (e.g. a world a friend sent) and
// imports the first folder inside it that has a level.dat.
func (m *Manager) ImportWorldZip(body io.Reader) error {
	if !m.isStopped() {
		return errors.New("stop the server first")
	}
	tmp, err := os.MkdirTemp(m.dataDir, "upload-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	zipPath := filepath.Join(tmp, "world.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, body)
	f.Close()
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	out := filepath.Join(tmp, "x")
	if err := extractZip(zipPath, out); err != nil {
		return fmt.Errorf("could not open the zip: %w", err)
	}
	os.Remove(zipPath)
	worldDir := ""
	depth := 99
	filepath.Walk(out, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == "level.dat" {
			if d := strings.Count(p, string(os.PathSeparator)); d < depth {
				worldDir, depth = filepath.Dir(p), d
			}
		}
		return nil
	})
	if worldDir == "" {
		return errors.New("the zip contains no Minecraft world (no level.dat inside)")
	}
	return m.importWorldDir(worldDir)
}

// NewWorld removes the current world (after a backup) so the next start
// generates a fresh one from the given seed and type.
func (m *Manager) NewWorld(seed, worldType string) error {
	if !m.isStopped() {
		return errors.New("stop the server first")
	}
	switch worldType {
	case "normal", "flat", "large_biomes", "amplified":
	default:
		return errors.New("unknown world type")
	}
	seed = strings.TrimSpace(seed)
	if len(seed) > 100 || strings.ContainsAny(seed, "\r\n") {
		return errors.New("the seed is too long")
	}
	if err := m.clearWorld(); err != nil {
		return err
	}
	c := m.cfg.Get()
	c.WorldSeed, c.WorldType = seed, worldType
	if err := m.cfg.Set(c); err != nil {
		return err
	}
	label := "a random seed"
	if seed != "" {
		label = "seed " + seed
	}
	m.log(fmt.Sprintf("[EasyHost] A new %s world with %s will be created the next time you press Start.", strings.ReplaceAll(worldType, "_", " "), label))
	return nil
}

func (m *Manager) backupDir() string { return filepath.Join(m.dataDir, "backups") }

var reBackupName = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.zip$`)

func (m *Manager) ListBackups() []BackupInfo {
	entries, _ := os.ReadDir(m.backupDir())
	var out []BackupInfo
	for _, e := range entries {
		if e.IsDir() || !reBackupName.MatchString(e.Name()) {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, BackupInfo{Name: e.Name(), SizeMB: info.Size() >> 20, Created: info.ModTime().UnixMilli()})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out
}

func (m *Manager) backupPath(name string) (string, error) {
	if !reBackupName.MatchString(name) {
		return "", errors.New("invalid backup name")
	}
	p := filepath.Join(m.backupDir(), name)
	if _, err := os.Stat(p); err != nil {
		return "", errors.New("backup not found")
	}
	return p, nil
}

// RestoreBackup puts a backup back in place. The world being replaced is
// backed up first, so a restore can be undone.
func (m *Manager) RestoreBackup(name string) error {
	if !m.isStopped() {
		return errors.New("stop the server first")
	}
	p, err := m.backupPath(name)
	if err != nil {
		return err
	}
	if err := m.clearWorld(); err != nil {
		return err
	}
	if err := extractZip(p, m.serverDir); err != nil {
		return fmt.Errorf("restoring failed: %w", err)
	}
	m.log("[EasyHost] Restored " + name + ".")
	return nil
}

func (m *Manager) DeleteBackup(name string) error {
	p, err := m.backupPath(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// copyDir copies a world folder, skipping the lock file the game holds.
func copyDir(src, dst string) (int, error) {
	n := 0
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if info.Name() == "session.lock" || !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(target)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		n++
		os.Chtimes(target, time.Now(), info.ModTime())
		return out.Close()
	})
	return n, err
}
