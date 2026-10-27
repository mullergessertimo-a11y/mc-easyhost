package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

// ensureJava makes sure a private Temurin JRE of the given major version is
// available in dataDir/java/<major> and returns the path to the java binary.
// Nothing is installed system-wide.
func ensureJava(dataDir string, major int, progress func(string, float64)) (string, error) {
	dir := filepath.Join(dataDir, "java", strconv.Itoa(major))
	if bin := findJavaBin(dir); bin != "" {
		return bin, nil
	}

	osName, ext := "linux", ".tar.gz"
	switch runtime.GOOS {
	case "windows":
		osName, ext = "windows", ".zip"
	case "darwin":
		osName = "mac"
	}
	arch := "x64"
	if runtime.GOARCH == "arm64" {
		arch = "aarch64"
	}

	var assets []struct {
		Binary struct {
			Package struct {
				Link     string `json:"link"`
				Checksum string `json:"checksum"`
			} `json:"package"`
		} `json:"binary"`
	}
	url := fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%d/hotspot?os=%s&architecture=%s&image_type=jre&vendor=eclipse", major, osName, arch)
	if err := getJSON(url, &assets); err != nil || len(assets) == 0 {
		// Some old versions have no JRE build for every platform; fall back to a JDK.
		url = fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%d/hotspot?os=%s&architecture=%s&image_type=jdk&vendor=eclipse", major, osName, arch)
		if err := getJSON(url, &assets); err != nil || len(assets) == 0 {
			return "", fmt.Errorf("could not find Java %d to download: %v", major, err)
		}
	}
	pkg := assets[0].Binary.Package

	archive := filepath.Join(dataDir, "java", fmt.Sprintf("java%d%s", major, ext))
	err := downloadFile(pkg.Link, archive, "sha256", pkg.Checksum, func(p float64) {
		progress(fmt.Sprintf("Downloading Java %d", major), p)
	})
	if err != nil {
		return "", err
	}
	progress(fmt.Sprintf("Unpacking Java %d", major), -1)
	tmp := dir + ".tmp"
	os.RemoveAll(tmp)
	if ext == ".zip" {
		err = extractZip(archive, tmp)
	} else {
		err = extractTarGz(archive, tmp)
	}
	os.Remove(archive)
	if err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return "", err
	}
	if bin := findJavaBin(dir); bin != "" {
		return bin, nil
	}
	return "", fmt.Errorf("java was downloaded but the executable was not found")
}

// findJavaBin looks for bin/java(.exe) up to two levels below dir (archives
// contain a top folder like jdk-21.0.5+11-jre).
func findJavaBin(dir string) string {
	name := "java"
	if runtime.GOOS == "windows" {
		name = "java.exe"
	}
	candidates := []string{filepath.Join(dir, "bin", name)}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				candidates = append(candidates,
					filepath.Join(dir, e.Name(), "bin", name),
					filepath.Join(dir, e.Name(), "Contents", "Home", "bin", name))
			}
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}
