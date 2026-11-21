package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Crossplay lets Bedrock players (phones, tablets, consoles, Windows
// Bedrock) join the Java server. Geyser translates between the two
// editions and Floodgate lets Bedrock players in without a Java account.
// On Paper both come from GeyserMC's own downloads (Floodgate isn't on
// Modrinth for Paper); on Fabric from Modrinth like any other mod.

const geyserAPI = "https://download.geysermc.org/v2/projects/"

// geyserProjectID marks plugins we got from GeyserMC in the mods manifest.
const geyserProjectID = "geysermc:"

// crossplayInstalled reports whether Geyser is in the server's mod folder.
func (m *Manager) crossplayInstalled(serverType string) bool {
	folder, err := modFolder(serverType)
	if err != nil {
		return false
	}
	for key, im := range m.loadModsManifest() {
		if strings.HasPrefix(key, folder+"/") && (im.Slug == "geyser" || im.ProjectID == geyserProjectID+"geyser") {
			if _, err := os.Stat(filepath.Join(m.serverDir, folder, im.File)); err == nil {
				return true
			}
		}
	}
	return false
}

// bedrockPort is the UDP port Geyser listens on, or 0 without crossplay.
func (m *Manager) bedrockPort(serverType string) int {
	if !m.crossplayInstalled(serverType) {
		return 0
	}
	port := 19132
	for _, p := range []string{
		filepath.Join(m.serverDir, "plugins", "Geyser-Spigot", "config.yml"),
		filepath.Join(m.serverDir, "config", "Geyser-Fabric", "config.yml"),
	} {
		if n := geyserConfigPort(p); n > 0 {
			port = n
		}
	}
	return port
}

// geyserConfigPort reads bedrock.port from Geyser's config.yml.
func geyserConfigPort(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	inBedrock := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inBedrock = trimmed == "bedrock:"
			continue
		}
		if v, ok := strings.CutPrefix(trimmed, "port:"); inBedrock && ok {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 && n < 65536 {
				return n
			}
		}
	}
	return 0
}

// EnableCrossplay installs Geyser and Floodgate.
func (m *Manager) EnableCrossplay() ([]string, error) {
	cfg := m.cfg.Get()
	switch cfg.ServerType {
	case "fabric":
		var got []string
		for _, slug := range []string{"geyser", "floodgate"} {
			names, err := m.InstallMod(slug)
			got = append(got, names...)
			if err != nil {
				return got, err
			}
		}
		return got, nil
	case "paper":
		modsMu.Lock()
		defer modsMu.Unlock()
		mf := m.loadModsManifest()
		var got []string
		for _, p := range []string{"geyser", "floodgate"} {
			name, err := m.installGeyserPlugin(mf, p)
			if name != "" {
				got = append(got, name)
			}
			if err != nil {
				m.saveModsManifest(mf)
				return got, err
			}
		}
		if len(got) > 0 && m.Status().State == StateRunning {
			m.log("[EasyHost] Restart the server to turn on crossplay.")
		}
		return got, m.saveModsManifest(mf)
	}
	return nil, errors.New("crossplay needs Paper or Fabric. Switch the server software in Settings first")
}

// installGeyserPlugin downloads the newest Paper build of Geyser or
// Floodgate from GeyserMC. It returns the title when something was installed.
func (m *Manager) installGeyserPlugin(mf map[string]InstalledMod, project string) (string, error) {
	var b struct {
		Version   string `json:"version"`
		Build     int    `json:"build"`
		Downloads map[string]struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"downloads"`
	}
	if err := getJSON(geyserAPI+project+"/versions/latest/builds/latest", &b); err != nil {
		return "", fmt.Errorf("could not reach GeyserMC: %w", err)
	}
	d, ok := b.Downloads["spigot"]
	if !ok || d.Name != filepath.Base(d.Name) || !strings.HasSuffix(d.Name, ".jar") {
		return "", fmt.Errorf("GeyserMC offers no Paper download of %s", project)
	}
	title := map[string]string{"geyser": "Geyser", "floodgate": "Floodgate"}[project]
	ver := fmt.Sprintf("%s-b%d", b.Version, b.Build)
	key := manifestKey("plugins", d.Name)
	if im, ok := mf[key]; ok && im.Version == ver {
		if _, err := os.Stat(filepath.Join(m.serverDir, "plugins", d.Name)); err == nil {
			return "", nil // already up to date
		}
	}
	m.log(fmt.Sprintf("[EasyHost] Installing %s %s", title, ver))
	url := fmt.Sprintf("%s%s/versions/%s/builds/%d/downloads/spigot", geyserAPI, project, b.Version, b.Build)
	if err := downloadFile(url, filepath.Join(m.serverDir, "plugins", d.Name), "sha256", d.SHA256, nil); err != nil {
		return "", fmt.Errorf("downloading %s failed: %w", title, err)
	}
	icon := "https://cdn.modrinth.com/data/wKkoqHrH/6e8e781d5669ecef9e19a73b5c2eed060d6f73e7_96.webp"
	if project == "floodgate" {
		icon = "https://cdn.modrinth.com/data/bWrNNfkb/851eeb9b5daf37baaebe7e8f25e8437735897c9c_96.webp"
	}
	mf[key] = InstalledMod{
		File: d.Name, ProjectID: geyserProjectID + project, Slug: project, Title: title, Icon: icon,
		Version: ver, ClientSide: "unsupported",
	}
	return title, nil
}
