package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Modpacks come in Modrinth's .mrpack format: a zip with modrinth.index.json
// (which mods to download, with hashes) plus config files in overrides/.
// We only install the parts a server needs and set Minecraft and the
// Fabric loader to exactly what the pack asks for.

type Modpack struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	ProjectID string `json:"projectId,omitempty"`
	Icon      string `json:"icon,omitempty"`
	Minecraft string `json:"minecraft"`
	Loader    string `json:"loader"`
	Installed string `json:"installed"` // when, for the UI
}

type mrpackIndex struct {
	FormatVersion int    `json:"formatVersion"`
	Game          string `json:"game"`
	Name          string `json:"name"`
	VersionID     string `json:"versionId"`
	Files         []struct {
		Path      string            `json:"path"`
		Hashes    map[string]string `json:"hashes"`
		Env       map[string]string `json:"env"`
		Downloads []string          `json:"downloads"`
		FileSize  int64             `json:"fileSize"`
	} `json:"files"`
	Dependencies map[string]string `json:"dependencies"`
}

// Hosts the .mrpack format allows downloads from.
var mrpackHosts = map[string]bool{
	"cdn.modrinth.com": true, "github.com": true, "raw.githubusercontent.com": true, "gitlab.com": true,
}

func (m *Manager) modpackPath() string { return filepath.Join(m.serverDir, "easyhost-modpack.json") }

// CurrentModpack returns the installed pack, if any.
func (m *Manager) CurrentModpack() *Modpack {
	b, err := os.ReadFile(m.modpackPath())
	if err != nil {
		return nil
	}
	var p Modpack
	if json.Unmarshal(b, &p) != nil || p.Name == "" {
		return nil
	}
	// Only while the server is still set up for it.
	if c := m.cfg.Get(); c.ServerType != "fabric" || c.Version != p.Minecraft {
		return nil
	}
	return &p
}

// SearchModpacks lists Fabric modpacks on Modrinth that work on a server.
func (m *Manager) SearchModpacks(query string, offset int) ([]ModHit, int, error) {
	facets, _ := json.Marshal([][]string{
		{"project_type:modpack"},
		{"categories:fabric"},
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
	cur := m.CurrentModpack()
	hits := make([]ModHit, 0, len(res.Hits))
	for _, h := range res.Hits {
		hits = append(hits, ModHit{ID: h.ProjectID, Slug: h.Slug, Title: h.Title, Description: h.Description,
			Icon: h.IconURL, Downloads: h.Downloads, ClientSide: h.ClientSide,
			Installed: cur != nil && cur.ProjectID == h.ProjectID})
	}
	return hits, res.TotalHits, nil
}

// InstallModpack installs the newest Fabric release of a Modrinth modpack.
func (m *Manager) InstallModpack(projectID string) (*Modpack, error) {
	var p modrinthProject
	if err := getJSON(modrinthAPI+"/project/"+url.PathEscape(projectID), &p); err != nil {
		return nil, fmt.Errorf("could not look up the modpack on Modrinth: %w", err)
	}
	q := url.Values{}
	q.Set("loaders", `["fabric"]`)
	var versions []modrinthVersion
	if err := getJSON(modrinthAPI+"/project/"+url.PathEscape(p.ID)+"/version?"+q.Encode(), &versions); err != nil {
		return nil, err
	}
	var v *modrinthVersion
	for _, kind := range []string{"release", "beta", "alpha"} {
		for i := range versions {
			if v == nil && versions[i].VersionType == kind && len(versions[i].Files) > 0 {
				v = &versions[i]
			}
		}
	}
	if v == nil {
		return nil, fmt.Errorf("%s has no Fabric version", p.Title)
	}
	file := v.Files[0]
	for _, f := range v.Files {
		if f.Primary {
			file = f
		}
	}
	if !strings.HasSuffix(file.Filename, ".mrpack") {
		return nil, fmt.Errorf("%s offers no .mrpack file", p.Title)
	}
	tmp := filepath.Join(m.dataDir, "tmp", "pack.mrpack")
	defer os.RemoveAll(filepath.Dir(tmp))
	if !m.beginPackInstall() {
		return nil, errors.New("stop the server first, then install the modpack")
	}
	defer m.endPackInstall()
	label := "Downloading " + p.Title
	if err := downloadFile(file.URL, tmp, "sha1", file.Hashes["sha1"], func(f float64) { m.packStep(label, f) }); err != nil {
		return nil, fmt.Errorf("downloading %s failed: %w", p.Title, err)
	}
	return m.installMrpack(tmp, Modpack{Name: p.Title, Version: v.VersionNumber, ProjectID: p.ID, Icon: p.IconURL})
}

// ImportModpack installs an uploaded .mrpack file.
func (m *Manager) ImportModpack(r io.Reader) (*Modpack, error) {
	if !m.beginPackInstall() {
		return nil, errors.New("stop the server first, then install the modpack")
	}
	defer m.endPackInstall()
	tmp := filepath.Join(m.dataDir, "tmp", "upload.mrpack")
	os.MkdirAll(filepath.Dir(tmp), 0o755)
	defer os.RemoveAll(filepath.Dir(tmp))
	f, err := os.Create(tmp)
	if err != nil {
		return nil, err
	}
	n, err := io.Copy(f, io.LimitReader(r, 1<<30))
	f.Close()
	if err != nil {
		return nil, err
	}
	if n >= 1<<30 {
		return nil, errors.New("that file is too big for a modpack")
	}
	return m.installMrpack(tmp, Modpack{})
}

// beginPackInstall marks the server busy so it can't start mid-install.
func (m *Manager) beginPackInstall() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.State != StateStopped && m.status.State != StateError {
		return false
	}
	m.status.State = StatePreparing
	m.status.Step = "Installing modpack"
	m.status.Progress = -1
	m.status.Error = ""
	go m.hub.SendStatus(m.snapshotLocked())
	return true
}

func (m *Manager) endPackInstall() {
	m.update(func(s *Status) { s.State = StateStopped; s.Step = ""; s.Progress = -1 })
}

func (m *Manager) packStep(text string, p float64) {
	m.update(func(s *Status) { s.Step = text; s.Progress = p })
}

// packPath checks a path from a modpack: relative, inside the server folder
// and not touching the world or our own files.
func (m *Manager) packPath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	clean := path.Clean(p)
	if p == "" || path.IsAbs(p) || strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(clean, ":") {
		return "", fmt.Errorf("modpack contains an unsafe path: %s", p)
	}
	top := strings.SplitN(clean, "/", 2)[0]
	switch top {
	case "world", "world_nether", "world_the_end", "eula.txt", "server.properties", "ops.json",
		"whitelist.json", "banned-players.json", "banned-ips.json":
		return "", nil // skip: the user's world and settings stay as they are
	}
	if strings.HasPrefix(top, "easyhost-") {
		return "", nil
	}
	return safeJoin(m.serverDir, filepath.FromSlash(clean))
}

func (m *Manager) installMrpack(file string, info Modpack) (*Modpack, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, errors.New("that is not a modpack file (.mrpack)")
	}
	defer zr.Close()
	var idx mrpackIndex
	for _, f := range zr.File {
		if f.Name == "modrinth.index.json" {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			err = json.NewDecoder(io.LimitReader(rc, 32<<20)).Decode(&idx)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("the modpack's index is broken: %w", err)
			}
		}
	}
	if idx.FormatVersion != 1 || idx.Game != "minecraft" {
		return nil, errors.New("that is not a Minecraft modpack (.mrpack from Modrinth)")
	}
	mc := idx.Dependencies["minecraft"]
	loader := idx.Dependencies["fabric-loader"]
	for _, other := range []string{"forge", "neoforge", "quilt-loader"} {
		if _, ok := idx.Dependencies[other]; ok && loader == "" {
			return nil, fmt.Errorf("this modpack needs %s, which MC EasyHost doesn't support yet. Pick a Fabric modpack", map[string]string{
				"forge": "Forge", "neoforge": "NeoForge", "quilt-loader": "Quilt"}[other])
		}
	}
	if mc == "" || loader == "" {
		return nil, errors.New("this modpack doesn't say which Minecraft and Fabric version it needs")
	}
	if info.Name == "" {
		info.Name = idx.Name
		info.Version = idx.VersionID
	}
	info.Minecraft, info.Loader = mc, loader

	// Check everything before touching the server folder.
	type job struct {
		url, dest, sha1, file string
	}
	var jobs []job
	for _, f := range idx.Files {
		if f.Env["server"] == "unsupported" {
			continue
		}
		dest, err := m.packPath(f.Path)
		if err != nil {
			return nil, err
		}
		if dest == "" {
			continue
		}
		if f.Hashes["sha1"] == "" || len(f.Downloads) == 0 {
			return nil, fmt.Errorf("modpack file %s has no checksum or download", f.Path)
		}
		u, err := url.Parse(f.Downloads[0])
		if err != nil || u.Scheme != "https" || !mrpackHosts[u.Hostname()] {
			return nil, fmt.Errorf("modpack file %s comes from a site modpacks may not use", f.Path)
		}
		jobs = append(jobs, job{f.Downloads[0], dest, f.Hashes["sha1"], path.Clean(strings.ReplaceAll(f.Path, "\\", "/"))})
	}

	m.log(fmt.Sprintf("[EasyHost] Installing modpack %s %s (Minecraft %s, Fabric loader %s, %d files)", info.Name, info.Version, mc, loader, len(jobs)))

	// Old mods are moved aside, not deleted.
	modsDir := filepath.Join(m.serverDir, "mods")
	if entries, _ := os.ReadDir(modsDir); len(entries) > 0 {
		aside := filepath.Join(m.dataDir, "backups", "mods-before-modpack-"+time.Now().Format("2006-01-02_15-04-05"))
		os.MkdirAll(filepath.Dir(aside), 0o755)
		if err := os.Rename(modsDir, aside); err != nil {
			return nil, fmt.Errorf("could not move the old mods aside: %w", err)
		}
		m.log("[EasyHost] Your previous mods were moved to " + aside)
	}

	// Download with a few connections at once; packs often have 100+ mods.
	var (
		wg       sync.Mutex
		done     int
		firstErr error
	)
	ch := make(chan job)
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := range ch {
				err := downloadFile(j.url, j.dest, "sha1", j.sha1, nil)
				wg.Lock()
				done++
				if err != nil && firstErr == nil {
					firstErr = fmt.Errorf("downloading %s failed: %w", j.file, err)
				}
				n := done
				wg.Unlock()
				m.packStep(fmt.Sprintf("Downloading %s: %d of %d files", info.Name, n, len(jobs)), float64(n)/float64(len(jobs)))
			}
		}()
	}
	for _, j := range jobs {
		wg.Lock()
		stop := firstErr != nil
		wg.Unlock()
		if stop {
			break
		}
		ch <- j
	}
	close(ch)
	workers.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	// Config files: overrides/ first, server-overrides/ wins.
	m.packStep("Copying the modpack's settings", -1)
	for _, prefix := range []string{"overrides/", "server-overrides/"} {
		for _, f := range zr.File {
			rel, ok := strings.CutPrefix(f.Name, prefix)
			if !ok || rel == "" || f.FileInfo().IsDir() {
				continue
			}
			dest, err := m.packPath(rel)
			if err != nil {
				return nil, err
			}
			if dest == "" {
				continue
			}
			if err := writeZipEntry(f, dest); err != nil {
				return nil, err
			}
		}
	}

	// Remember the pack's mods so the Mods tab can label them.
	mf := m.loadModsManifest()
	for key := range mf {
		if strings.HasPrefix(key, "mods/") {
			delete(mf, key)
		}
	}
	for _, j := range jobs {
		if dir, name := path.Split(j.file); dir == "mods/" && strings.HasSuffix(strings.ToLower(name), ".jar") {
			mf[manifestKey("mods", name)] = InstalledMod{File: name, Title: prettyJarName(name), GameVersion: mc, Pack: info.Name}
		}
	}
	if err := m.saveModsManifest(mf); err != nil {
		return nil, err
	}

	c := m.cfg.Get()
	c.ServerType, c.Version, c.FabricLoader = "fabric", mc, loader
	if err := m.cfg.Set(c); err != nil {
		return nil, err
	}
	info.Installed = time.Now().Format("2006-01-02 15:04")
	b, _ := json.MarshalIndent(info, "", "  ")
	os.WriteFile(m.modpackPath(), b, 0o644)
	m.log(fmt.Sprintf("[EasyHost] Modpack %s is installed. Players need the same modpack in their launcher (e.g. Modrinth App or Prism).", info.Name))
	return &info, nil
}

func writeZipEntry(f *zip.File, dest string) error {
	os.MkdirAll(filepath.Dir(dest), 0o755)
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, rc)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// prettyJarName turns "sodium-fabric-0.5.8+mc1.20.4.jar" into "sodium fabric".
func prettyJarName(name string) string {
	base := strings.TrimSuffix(name, ".jar")
	parts := strings.FieldsFunc(base, func(r rune) bool { return r == '-' || r == '_' })
	var words []string
	for _, p := range parts {
		if p == "" || (p[0] >= '0' && p[0] <= '9') || strings.HasPrefix(p, "mc1") || strings.HasPrefix(p, "v1") ||
			strings.HasPrefix(strings.ToLower(p), "fabric") && len(words) > 0 {
			break
		}
		words = append(words, p)
	}
	if len(words) == 0 {
		return base
	}
	return strings.Join(words, " ")
}
