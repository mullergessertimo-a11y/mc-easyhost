package main

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ServerBuild is a concrete jar to download for a version.
type ServerBuild struct {
	Version   string
	JarName   string // file name inside the server folder
	URL       string
	HashAlgo  string
	Hash      string
	JavaMajor int
}

type mojangManifest struct {
	Latest struct {
		Release string `json:"release"`
	} `json:"latest"`
	Versions []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"versions"`
}

type mojangVersion struct {
	JavaVersion struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
	Downloads struct {
		Server struct {
			SHA1 string `json:"sha1"`
			URL  string `json:"url"`
		} `json:"server"`
	} `json:"downloads"`
}

var (
	manifestMu    sync.Mutex
	manifestCache *mojangManifest
	manifestTime  time.Time
)

func getManifest() (*mojangManifest, error) {
	manifestMu.Lock()
	defer manifestMu.Unlock()
	if manifestCache != nil && time.Since(manifestTime) < time.Hour {
		return manifestCache, nil
	}
	var m mojangManifest
	if err := getJSON("https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &m); err != nil {
		return nil, err
	}
	manifestCache, manifestTime = &m, time.Now()
	return &m, nil
}

// releaseVersions lists release ids, newest first, that the given server
// type can actually run.
func releaseVersions(serverType string) ([]string, error) {
	m, err := getManifest()
	if err != nil {
		return nil, err
	}
	var supported map[string]bool
	switch serverType {
	case "paper":
		supported, err = paperVersions()
	case "fabric":
		supported, err = fabricGameVersions()
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, v := range m.Versions {
		if v.Type != "release" {
			continue
		}
		if supported != nil && !supported[v.ID] {
			continue
		}
		out = append(out, v.ID)
		// Very old versions are not worth offering in a one-click tool.
		if v.ID == "1.8.8" || v.ID == "1.8.9" {
			break
		}
	}
	return out, nil
}

func paperVersions() (map[string]bool, error) {
	var p struct {
		Versions map[string][]string `json:"versions"`
	}
	if err := getJSON("https://fill.papermc.io/v3/projects/paper", &p); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, list := range p.Versions {
		for _, v := range list {
			if !strings.ContainsAny(v, "-") { // skip rc/pre builds
				out[v] = true
			}
		}
	}
	return out, nil
}

// resolveBuild turns the user's choice into a concrete download.
func resolveBuild(serverType, version string) (*ServerBuild, error) {
	m, err := getManifest()
	if err != nil {
		return nil, fmt.Errorf("could not reach Mojang (are you online?): %w", err)
	}
	if version == "latest" {
		if serverType != "vanilla" {
			list, err := releaseVersions(serverType)
			if err != nil || len(list) == 0 {
				return nil, fmt.Errorf("could not reach %s: %v", typeLabel(serverType), err)
			}
			version = list[0]
		} else {
			version = m.Latest.Release
		}
	}
	var metaURL string
	for _, v := range m.Versions {
		if v.ID == version {
			metaURL = v.URL
		}
	}
	if metaURL == "" {
		return nil, fmt.Errorf("unknown Minecraft version %q", version)
	}
	var mv mojangVersion
	if err := getJSON(metaURL, &mv); err != nil {
		return nil, err
	}
	java := mv.JavaVersion.MajorVersion
	if java == 0 {
		java = 8
	}
	// Adoptium only ships LTS JREs; round odd versions (e.g. 16) up.
	java = ltsJava(java)

	if serverType == "fabric" {
		loader, installer, err := fabricLoaderAndInstaller()
		if err != nil {
			return nil, fmt.Errorf("could not reach Fabric: %w", err)
		}
		// The launcher jar downloads the matching vanilla server on first start.
		return &ServerBuild{
			Version: version,
			JarName: fmt.Sprintf("fabric-server-mc.%s-loader.%s-launcher.%s.jar", version, loader, installer),
			URL: fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar",
				url.PathEscape(version), url.PathEscape(loader), url.PathEscape(installer)),
			JavaMajor: java,
		}, nil
	}

	if serverType == "vanilla" {
		if mv.Downloads.Server.URL == "" {
			return nil, fmt.Errorf("Mojang offers no server download for %s", version)
		}
		return &ServerBuild{
			Version: version, JarName: "server-" + version + ".jar",
			URL: mv.Downloads.Server.URL, HashAlgo: "sha1", Hash: mv.Downloads.Server.SHA1,
			JavaMajor: java,
		}, nil
	}

	var b struct {
		ID        int `json:"id"`
		Downloads map[string]struct {
			Name      string            `json:"name"`
			Checksums map[string]string `json:"checksums"`
			URL       string            `json:"url"`
		} `json:"downloads"`
	}
	if err := getJSON("https://fill.papermc.io/v3/projects/paper/versions/"+version+"/builds/latest", &b); err != nil {
		return nil, fmt.Errorf("PaperMC has no build for %s: %w", version, err)
	}
	d, ok := b.Downloads["server:default"]
	if !ok {
		return nil, fmt.Errorf("PaperMC build for %s has no server download", version)
	}
	return &ServerBuild{
		Version: version, JarName: d.Name, URL: d.URL,
		HashAlgo: "sha256", Hash: d.Checksums["sha256"], JavaMajor: java,
	}, nil
}

func ltsJava(v int) int {
	for _, lts := range []int{8, 11, 17, 21, 25} {
		if v <= lts {
			return lts
		}
	}
	return v
}

// fabricGameVersions lists the stable Minecraft versions Fabric supports.
func fabricGameVersions() (map[string]bool, error) {
	var list []struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	if err := getJSON("https://meta.fabricmc.net/v2/versions/game", &list); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, v := range list {
		if v.Stable {
			out[v.Version] = true
		}
	}
	return out, nil
}

// fabricLoaderAndInstaller returns the newest stable loader and installer.
func fabricLoaderAndInstaller() (loader, installer string, err error) {
	type entry struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
	}
	newestStable := func(path string) (string, error) {
		var list []entry
		if err := getJSON("https://meta.fabricmc.net/v2/versions/"+path, &list); err != nil {
			return "", err
		}
		for _, e := range list {
			if e.Stable {
				return e.Version, nil
			}
		}
		if len(list) > 0 {
			return list[0].Version, nil
		}
		return "", fmt.Errorf("no Fabric %s found", path)
	}
	if loader, err = newestStable("loader"); err != nil {
		return
	}
	installer, err = newestStable("installer")
	return
}
