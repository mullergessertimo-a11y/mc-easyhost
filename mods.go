package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Mods (Fabric) and plugins (Paper) come from Modrinth, the main open mod
// site. What we installed is remembered in easyhost-mods.json so the UI can
// show names and icons and reinstall everything for a new Minecraft version.

const modrinthAPI = "https://api.modrinth.com/v2"

type InstalledMod struct {
	File        string `json:"file"`
	ProjectID   string `json:"projectId"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Icon        string `json:"icon"`
	Version     string `json:"version"`     // the mod's own version number
	GameVersion string `json:"gameVersion"` // Minecraft version it was installed for
	ClientSide  string `json:"clientSide"`  // required / optional / unsupported
	Dependency  bool   `json:"dependency"`  // pulled in by another mod
	Unknown     bool   `json:"unknown"`     // jar dropped in by hand
}

type ModHit struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Downloads   int    `json:"downloads"`
	ClientSide  string `json:"clientSide"`
	Installed   bool   `json:"installed"`
}

var modsMu sync.Mutex // one install/remove at a time

// modFolder is where the server software looks for add-ons.
func modFolder(serverType string) (string, error) {
	switch serverType {
	case "paper":
		return "plugins", nil
	case "fabric":
		return "mods", nil
	}
	return "", errors.New("vanilla Minecraft can't load mods or plugins. Switch the server software to Paper (plugins) or Fabric (mods) in Settings")
}

func modLoaders(serverType string) []string {
	if serverType == "fabric" {
		return []string{"fabric"}
	}
	return []string{"paper", "spigot", "bukkit"}
}

// targetVersion is the Minecraft version the next start will use.
func (m *Manager) targetVersion() (string, error) {
	cfg := m.cfg.Get()
	if cfg.Version != "latest" {
		return cfg.Version, nil
	}
	list, err := releaseVersions(cfg.ServerType)
	if err != nil || len(list) == 0 {
		return "", fmt.Errorf("could not look up the newest version: %v", err)
	}
	return list[0], nil
}

func (m *Manager) modsManifestPath() string {
	return filepath.Join(m.serverDir, "easyhost-mods.json")
}

func (m *Manager) loadModsManifest() map[string]InstalledMod {
	out := map[string]InstalledMod{}
	if b, err := os.ReadFile(m.modsManifestPath()); err == nil {
		json.Unmarshal(b, &out)
	}
	return out
}

func (m *Manager) saveModsManifest(mf map[string]InstalledMod) error {
	os.MkdirAll(m.serverDir, 0o755)
	b, _ := json.MarshalIndent(mf, "", "  ")
	return os.WriteFile(m.modsManifestPath(), b, 0o644)
}

// manifestKey keeps paper plugins and fabric mods apart.
func manifestKey(folder, file string) string { return folder + "/" + file }

// ListMods returns what is in the mod folder for the current server type.
func (m *Manager) ListMods() ([]InstalledMod, string, error) {
	cfg := m.cfg.Get()
	folder, err := modFolder(cfg.ServerType)
	if err != nil {
		return nil, "", err
	}
	mf := m.loadModsManifest()
	entries, _ := os.ReadDir(filepath.Join(m.serverDir, folder))
	var out []InstalledMod
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".jar") {
			continue
		}
		if im, ok := mf[manifestKey(folder, e.Name())]; ok {
			out = append(out, im)
		} else {
			out = append(out, InstalledMod{File: e.Name(), Title: strings.TrimSuffix(e.Name(), ".jar"), Unknown: true})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dependency != out[j].Dependency {
			return !out[i].Dependency
		}
		return strings.ToLower(out[i].Title) < strings.ToLower(out[j].Title)
	})
	return out, folder, nil
}

// SearchMods asks Modrinth for server-compatible mods/plugins.
func (m *Manager) SearchMods(query string, offset int) ([]ModHit, int, error) {
	cfg := m.cfg.Get()
	if _, err := modFolder(cfg.ServerType); err != nil {
		return nil, 0, err
	}
	version, err := m.targetVersion()
	if err != nil {
		return nil, 0, err
	}
	var loaderFacet []string
	for _, l := range modLoaders(cfg.ServerType) {
		loaderFacet = append(loaderFacet, "categories:"+l)
	}
	projectType := "mod"
	if cfg.ServerType == "paper" {
		projectType = "plugin"
	}
	facets, _ := json.Marshal([][]string{
		loaderFacet,
		{"versions:" + version},
		{"project_type:" + projectType},
		{"server_side:required", "server_side:optional"},
	})
	q := url.Values{}
	q.Set("query", query)
	q.Set("facets", string(facets))
	q.Set("limit", "20")
	q.Set("offset", strconv.Itoa(offset))
	if query == "" {
		q.Set("index", "downloads")
	}
	var res struct {
		Hits []struct {
			ProjectID   string `json:"project_id"`
			Slug        string `json:"slug"`
			Title       string `json:"title"`
			Description string `json:"description"`
			IconURL     string `json:"icon_url"`
			Downloads   int    `json:"downloads"`
			ClientSide  string `json:"client_side"`
		} `json:"hits"`
		TotalHits int `json:"total_hits"`
	}
	if err := getJSON(modrinthAPI+"/search?"+q.Encode(), &res); err != nil {
		return nil, 0, fmt.Errorf("could not reach Modrinth: %w", err)
	}
	installed := map[string]bool{}
	for _, im := range m.loadModsManifest() {
		installed[im.ProjectID] = true
	}
	hits := make([]ModHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		hits = append(hits, ModHit{
			ID: h.ProjectID, Slug: h.Slug, Title: h.Title, Description: h.Description,
			Icon: h.IconURL, Downloads: h.Downloads, ClientSide: h.ClientSide,
			Installed: installed[h.ProjectID],
		})
	}
	return hits, res.TotalHits, nil
}

type modrinthVersion struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	VersionNumber string   `json:"version_number"`
	VersionType   string   `json:"version_type"`
	Loaders       []string `json:"loaders"`
	Files         []struct {
		URL      string            `json:"url"`
		Filename string            `json:"filename"`
		Primary  bool              `json:"primary"`
		Hashes   map[string]string `json:"hashes"`
	} `json:"files"`
	Dependencies []struct {
		ProjectID      string `json:"project_id"`
		VersionID      string `json:"version_id"`
		DependencyType string `json:"dependency_type"`
	} `json:"dependencies"`
}

type modrinthProject struct {
	ID         string `json:"id"`
	Slug       string `json:"slug"`
	Title      string `json:"title"`
	IconURL    string `json:"icon_url"`
	ClientSide string `json:"client_side"`
}

// bestVersion picks the newest release build for the loader and game
// version, falling back to beta/alpha when a mod has no release yet.
func bestVersion(projectID string, loaders []string, gameVersion string) (*modrinthVersion, error) {
	l, _ := json.Marshal(loaders)
	g, _ := json.Marshal([]string{gameVersion})
	q := url.Values{}
	q.Set("loaders", string(l))
	q.Set("game_versions", string(g))
	var versions []modrinthVersion
	if err := getJSON(modrinthAPI+"/project/"+url.PathEscape(projectID)+"/version?"+q.Encode(), &versions); err != nil {
		return nil, err
	}
	for _, kind := range []string{"release", "beta", "alpha"} {
		for i := range versions {
			if versions[i].VersionType == kind && len(versions[i].Files) > 0 {
				return &versions[i], nil
			}
		}
	}
	return nil, nil
}

// InstallMod installs a project and its required dependencies.
func (m *Manager) InstallMod(projectID string) ([]string, error) {
	modsMu.Lock()
	defer modsMu.Unlock()
	cfg := m.cfg.Get()
	folder, err := modFolder(cfg.ServerType)
	if err != nil {
		return nil, err
	}
	version, err := m.targetVersion()
	if err != nil {
		return nil, err
	}
	mf := m.loadModsManifest()
	var installed []string
	seen := map[string]bool{}
	var install func(id string, dep bool) error
	install = func(id string, dep bool) error {
		if seen[id] {
			return nil
		}
		seen[id] = true
		var p modrinthProject
		if err := getJSON(modrinthAPI+"/project/"+url.PathEscape(id), &p); err != nil {
			return fmt.Errorf("could not look up %s on Modrinth: %w", id, err)
		}
		v, err := bestVersion(p.ID, modLoaders(cfg.ServerType), version)
		if err != nil {
			return fmt.Errorf("could not look up versions of %s: %w", p.Title, err)
		}
		if v == nil {
			return fmt.Errorf("%s has no version for %s %s yet", p.Title, typeLabel(cfg.ServerType), version)
		}
		file := v.Files[0]
		for _, f := range v.Files {
			if f.Primary {
				file = f
			}
		}
		name := filepath.Base(file.Filename)
		if !strings.HasSuffix(strings.ToLower(name), ".jar") || name != file.Filename {
			return fmt.Errorf("%s offers an unexpected file %q", p.Title, file.Filename)
		}

		// Already there in this exact version? Keep it, but don't demote a
		// mod the user chose to a dependency.
		for key, im := range mf {
			if im.ProjectID == p.ID && strings.HasPrefix(key, folder+"/") {
				if _, statErr := os.Stat(filepath.Join(m.serverDir, folder, im.File)); im.File == name && statErr == nil {
					if !dep && im.Dependency {
						im.Dependency = false
						mf[key] = im
					}
					return installDeps(v, install)
				}
				os.Remove(filepath.Join(m.serverDir, folder, im.File)) // older version
				delete(mf, key)
			}
		}

		dest := filepath.Join(m.serverDir, folder, name)
		m.log(fmt.Sprintf("[EasyHost] Installing %s %s", p.Title, v.VersionNumber))
		if err := downloadFile(file.URL, dest, "sha1", file.Hashes["sha1"], nil); err != nil {
			return fmt.Errorf("downloading %s failed: %w", p.Title, err)
		}
		mf[manifestKey(folder, name)] = InstalledMod{
			File: name, ProjectID: p.ID, Slug: p.Slug, Title: p.Title, Icon: p.IconURL,
			Version: v.VersionNumber, GameVersion: version, ClientSide: p.ClientSide, Dependency: dep,
		}
		installed = append(installed, p.Title)
		return installDeps(v, install)
	}
	err = install(projectID, false)
	if serr := m.saveModsManifest(mf); err == nil {
		err = serr
	}
	if len(installed) > 0 && m.Status().State == StateRunning {
		m.log("[EasyHost] Restart the server to load the new " + folder + ".")
	}
	return installed, err
}

func installDeps(v *modrinthVersion, install func(string, bool) error) error {
	for _, d := range v.Dependencies {
		if d.DependencyType != "required" {
			continue
		}
		id := d.ProjectID
		if id == "" && d.VersionID != "" {
			var dv modrinthVersion
			if err := getJSON(modrinthAPI+"/version/"+url.PathEscape(d.VersionID), &dv); err != nil {
				return err
			}
			id = dv.ProjectID
		}
		if id != "" {
			if err := install(id, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// RemoveMod deletes one jar. Windows locks jars of a running server, so
// this only works while it is stopped.
func (m *Manager) RemoveMod(file string) error {
	modsMu.Lock()
	defer modsMu.Unlock()
	if !m.isStopped() {
		return errors.New("stop the server first, then remove it")
	}
	folder, err := modFolder(m.cfg.Get().ServerType)
	if err != nil {
		return err
	}
	if file != filepath.Base(file) || !strings.HasSuffix(strings.ToLower(file), ".jar") {
		return errors.New("invalid file name")
	}
	if err := os.Remove(filepath.Join(m.serverDir, folder, file)); err != nil && !os.IsNotExist(err) {
		return err
	}
	mf := m.loadModsManifest()
	delete(mf, manifestKey(folder, file))
	m.log("[EasyHost] Removed " + file)
	return m.saveModsManifest(mf)
}

// UpdateMods reinstalls every mod we know for the version the next start
// will use, e.g. after switching to a newer Minecraft version.
func (m *Manager) UpdateMods() ([]string, []string) {
	if !m.isStopped() {
		return nil, []string{"stop the server first"}
	}
	folder, err := modFolder(m.cfg.Get().ServerType)
	if err != nil {
		return nil, []string{err.Error()}
	}
	var ids []string
	for key, im := range m.loadModsManifest() {
		if strings.HasPrefix(key, folder+"/") && !im.Dependency {
			ids = append(ids, im.ProjectID)
		}
	}
	var updated, problems []string
	for _, id := range ids {
		got, err := m.InstallMod(id)
		updated = append(updated, got...)
		if err != nil {
			problems = append(problems, err.Error())
		}
	}
	return updated, problems
}

// warnModVersions is called on start so mismatches are explained instead
// of surfacing as a cryptic crash.
func (m *Manager) warnModVersions(serverType, version string) {
	folder, err := modFolder(serverType)
	if err != nil {
		return
	}
	var stale []string
	for key, im := range m.loadModsManifest() {
		if strings.HasPrefix(key, folder+"/") && im.GameVersion != "" && im.GameVersion != version {
			stale = append(stale, fmt.Sprintf("%s (for %s)", im.Title, im.GameVersion))
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		m.log(fmt.Sprintf("[EasyHost] Warning: these %s were installed for another Minecraft version and may not load on %s: %s. Use \"Update all\" on the Mods tab.",
			folder, version, strings.Join(stale, ", ")))
	}
}

// extraUDPPorts returns ports that installed mods need opened, today only
// Simple Voice Chat (UDP 24454 unless its config says otherwise).
func (m *Manager) extraUDPPorts(serverType string, mcPort int) []int {
	folder, err := modFolder(serverType)
	if err != nil {
		return nil
	}
	hasVoice := false
	for key, im := range m.loadModsManifest() {
		if strings.HasPrefix(key, folder+"/") && im.Slug == "simple-voice-chat" {
			hasVoice = true
		}
	}
	if !hasVoice {
		return nil
	}
	port := 24454
	for _, p := range []string{
		filepath.Join(m.serverDir, "config", "voicechat", "voicechat-server.properties"),
		filepath.Join(m.serverDir, "plugins", "voicechat", "voicechat-server.properties"),
	} {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if v, ok := strings.CutPrefix(strings.TrimSpace(sc.Text()), "port="); ok {
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					port = n
				}
			}
		}
		f.Close()
	}
	if port == -1 { // voice chat shares the Minecraft port number (still UDP)
		port = mcPort
	}
	if port <= 0 || port > 65535 {
		return nil
	}
	return []int{port}
}

func (m *Manager) isStopped() bool {
	st := m.Status().State
	return st == StateStopped || st == StateError
}
