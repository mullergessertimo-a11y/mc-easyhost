package main

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type State string

const (
	StateStopped   State = "stopped"
	StatePreparing State = "preparing" // downloading Java / server jar
	StateStarting  State = "starting"  // java is running, world is loading
	StateRunning   State = "running"   // "Done (...)!" was printed
	StateStopping  State = "stopping"
	StateError     State = "error"
)

type Status struct {
	State    State    `json:"state"`
	Step     string   `json:"step"`     // human readable current step
	Progress float64  `json:"progress"` // 0..1, or -1 for indeterminate
	Error    string   `json:"error"`
	Version  string   `json:"version"`
	Type     string   `json:"type"`
	MemoryMB int      `json:"memoryMB"`
	Players  []string `json:"players"`
	Started  int64    `json:"started"` // unix ms when it became running
	Net      NetInfo  `json:"net"`
}

type Manager struct {
	dataDir   string
	serverDir string
	cfg       *ConfigStore
	hub       *Hub

	mu      sync.Mutex
	status  Status
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	players map[string]bool
	stopReq bool
	exited  chan struct{}
	mapping *portMapping
}

func NewManager(dataDir string, cfg *ConfigStore, hub *Hub) *Manager {
	m := &Manager{
		dataDir:   dataDir,
		serverDir: filepath.Join(dataDir, "server"),
		cfg:       cfg,
		hub:       hub,
		players:   map[string]bool{},
	}
	m.status.State = StateStopped
	m.status.Progress = -1
	return m
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *Manager) snapshotLocked() Status {
	s := m.status
	s.Players = make([]string, 0, len(m.players))
	for p := range m.players {
		s.Players = append(s.Players, p)
	}
	sort.Strings(s.Players)
	return s
}

// update mutates the status under the lock and pushes it to all UIs.
func (m *Manager) update(f func(s *Status)) {
	m.mu.Lock()
	f(&m.status)
	s := m.snapshotLocked()
	m.mu.Unlock()
	m.hub.SendStatus(s)
}

func (m *Manager) step(text string, progress float64) {
	m.update(func(s *Status) { s.Step = text; s.Progress = progress })
}

func (m *Manager) log(line string) { m.hub.SendLog(line) }

var ErrEula = errors.New("eula")

// Start runs the whole "one button" pipeline in the background.
func (m *Manager) Start() error {
	m.mu.Lock()
	if m.status.State != StateStopped && m.status.State != StateError {
		m.mu.Unlock()
		return fmt.Errorf("server is already %s", m.status.State)
	}
	if !m.cfg.Get().EulaAccepted {
		m.mu.Unlock()
		return ErrEula
	}
	m.status = Status{State: StatePreparing, Step: "Getting ready", Progress: -1, Net: m.status.Net}
	m.players = map[string]bool{}
	m.stopReq = false
	m.mu.Unlock()
	m.hub.SendStatus(m.Status())

	go func() {
		if err := m.run(); err != nil {
			m.log("[EasyHost] ERROR: " + err.Error())
			m.update(func(s *Status) { s.State = StateError; s.Error = err.Error(); s.Step = ""; s.Progress = -1 })
		}
	}()
	return nil
}

type buildRecord struct {
	ServerBuild
	Type string
}

func (m *Manager) run() error {
	cfg := m.cfg.Get()
	if err := os.MkdirAll(m.serverDir, 0o755); err != nil {
		return err
	}

	m.step("Checking for the newest Minecraft version", -1)
	recordPath := filepath.Join(m.serverDir, "easyhost-build.json")
	build, err := resolveBuild(cfg.ServerType, cfg.Version)
	if err != nil {
		// Offline? Reuse whatever we ran last time, if it matches the choice.
		var rec buildRecord
		b, rerr := os.ReadFile(recordPath)
		if rerr != nil || json.Unmarshal(b, &rec) != nil || rec.Type != cfg.ServerType ||
			(cfg.Version != "latest" && cfg.Version != rec.Version) {
			return err
		}
		m.log("[EasyHost] Could not check for updates (" + err.Error() + "), using the version from last time.")
		build = &rec.ServerBuild
	}
	m.update(func(s *Status) { s.Version = build.Version; s.Type = cfg.ServerType })

	// Protect worlds from accidental downgrades, which corrupt them.
	if prev := lastVersion(recordPath); prev != "" && compareVersions(build.Version, prev) < 0 && worldExists(m.serverDir) {
		return fmt.Errorf("your world was last played on %s; starting it on older %s could break it. Pick %s or newer, or make a backup and delete the world first", prev, build.Version, prev)
	}

	java, err := ensureJava(m.dataDir, build.JavaMajor, func(text string, p float64) { m.step(text, p) })
	if err != nil {
		return fmt.Errorf("installing Java failed: %w", err)
	}

	jar := filepath.Join(m.serverDir, build.JarName)
	if _, err := os.Stat(jar); err != nil {
		label := fmt.Sprintf("Downloading %s %s", typeLabel(cfg.ServerType), build.Version)
		if err := downloadFile(build.URL, jar, build.HashAlgo, build.Hash, func(p float64) { m.step(label, p) }); err != nil {
			return fmt.Errorf("downloading the server failed: %w", err)
		}
		m.cleanupOldJars(build.JarName)
	}
	if b, err := json.Marshal(buildRecord{*build, cfg.ServerType}); err == nil {
		os.WriteFile(recordPath, b, 0o644)
	}

	m.step("Writing settings", -1)
	// The user ticked "I agree" in the UI; only then do we write this.
	if err := os.WriteFile(filepath.Join(m.serverDir, "eula.txt"),
		[]byte("# Accepted by the user in MC EasyHost (https://aka.ms/MinecraftEULA)\neula=true\n"), 0o644); err != nil {
		return err
	}
	if err := writeServerProperties(filepath.Join(m.serverDir, "server.properties"), cfg); err != nil {
		return err
	}

	mem := cfg.MemoryMB
	if mem == 0 {
		mem = autoMemoryMB()
	}
	m.update(func(s *Status) { s.MemoryMB = mem })

	// Networking is best effort and must not delay the start.
	go m.setupNetwork(cfg.Port, cfg.UPnP, java)

	args := []string{fmt.Sprintf("-Xms%dM", mem), fmt.Sprintf("-Xmx%dM", mem)}
	if cfg.ServerType == "paper" && build.JavaMajor >= 17 {
		args = append(args, aikarFlags(mem)...)
	}
	args = append(args, "-Dfile.encoding=UTF-8", "-jar", build.JarName, "nogui")

	m.step("Starting the server", -1)
	m.log(fmt.Sprintf("[EasyHost] Starting %s %s with %d MB RAM (Java %d)", typeLabel(cfg.ServerType), build.Version, mem, build.JavaMajor))
	cmd := exec.Command(java, args...)
	cmd.Dir = m.serverDir
	setupChildProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start Java: %w", err)
	}
	attachChildProcess(cmd)

	exited := make(chan struct{})
	m.mu.Lock()
	m.cmd, m.stdin, m.exited = cmd, stdin, exited
	m.status.State = StateStarting
	m.status.Step = "Loading the world"
	m.mu.Unlock()
	m.hub.SendStatus(m.Status())

	go m.readOutput(pr)
	go func() {
		err := cmd.Wait()
		pw.Close()
		m.onExit(err)
		close(exited)
	}()
	return nil
}

var (
	reDone   = regexp.MustCompile(`\]: Done \(`)
	reJoin   = regexp.MustCompile(`\]: ([A-Za-z0-9_.]{2,16}) joined the game`)
	reLeave  = regexp.MustCompile(`\]: ([A-Za-z0-9_.]{2,16}) left the game`)
	rePort   = regexp.MustCompile(`(?i)FAILED TO BIND TO PORT|Address already in use`)
)

func (m *Manager) readOutput(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := stripANSI(sc.Text())
		m.log(line)
		switch {
		case reDone.MatchString(line):
			m.update(func(s *Status) {
				if s.State == StateStarting {
					s.State = StateRunning
					s.Step = ""
					s.Started = time.Now().UnixMilli()
				}
			})
		case reJoin.MatchString(line):
			name := reJoin.FindStringSubmatch(line)[1]
			m.mu.Lock()
			m.players[name] = true
			m.mu.Unlock()
			m.hub.SendStatus(m.Status())
		case reLeave.MatchString(line):
			name := reLeave.FindStringSubmatch(line)[1]
			m.mu.Lock()
			delete(m.players, name)
			m.mu.Unlock()
			m.hub.SendStatus(m.Status())
		case rePort.MatchString(line):
			m.log(fmt.Sprintf("[EasyHost] The port is already used by another program (maybe another Minecraft server is running). Close it or change the port in Settings."))
		}
	}
	io.Copy(io.Discard, r)
}

var reANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func stripANSI(s string) string { return reANSI.ReplaceAllString(s, "") }

func (m *Manager) onExit(err error) {
	m.mu.Lock()
	requested := m.stopReq
	wasRunning := m.status.State == StateRunning
	m.cmd, m.stdin = nil, nil
	m.players = map[string]bool{}
	mapping := m.mapping
	m.mapping = nil
	m.mu.Unlock()

	if mapping != nil {
		mapping.Remove()
	}
	if requested || err == nil {
		m.log("[EasyHost] Server stopped.")
		m.update(func(s *Status) { s.State = StateStopped; s.Step = ""; s.Started = 0; s.Net.UPnP = "" })
		return
	}
	msg := "the server stopped unexpectedly"
	if !wasRunning {
		msg = "the server crashed while starting; check the console for the reason"
	}
	m.log("[EasyHost] " + msg + " (" + err.Error() + ")")
	m.update(func(s *Status) { s.State = StateError; s.Error = msg; s.Step = ""; s.Started = 0; s.Net.UPnP = "" })
}

// Stop asks the server to save and shut down; kills it if it hangs.
func (m *Manager) Stop() {
	m.mu.Lock()
	stdin, exited := m.stdin, m.exited
	if m.cmd == nil || m.stopReq {
		m.mu.Unlock()
		return
	}
	m.stopReq = true
	m.status.State = StateStopping
	m.status.Step = "Saving the world and stopping"
	cmd := m.cmd
	m.mu.Unlock()
	m.hub.SendStatus(m.Status())

	io.WriteString(stdin, "stop\n")
	go func() {
		select {
		case <-exited:
		case <-time.After(90 * time.Second):
			m.log("[EasyHost] Server did not stop within 90 seconds, forcing it to close.")
			cmd.Process.Kill()
		}
	}()
}

// StopAndWait is used when the app is closing.
func (m *Manager) StopAndWait() {
	m.mu.Lock()
	exited := m.exited
	running := m.cmd != nil
	m.mu.Unlock()
	if !running {
		return
	}
	m.Stop()
	select {
	case <-exited:
	case <-time.After(100 * time.Second):
	}
}

func (m *Manager) Command(c string) error {
	m.mu.Lock()
	stdin := m.stdin
	m.mu.Unlock()
	if stdin == nil {
		return errors.New("the server is not running")
	}
	c = strings.TrimPrefix(strings.TrimSpace(c), "/")
	if c == "" || strings.ContainsAny(c, "\r\n") {
		return errors.New("invalid command")
	}
	m.log("> " + c)
	_, err := io.WriteString(stdin, c+"\n")
	return err
}

func (m *Manager) setupNetwork(port int, upnp bool, java string) {
	info := NetInfo{LanIP: lanIP(), Port: port}
	m.update(func(s *Status) { s.Net = info })

	if msg := ensureFirewall(java, port); msg != "" {
		m.log("[EasyHost] " + msg)
	}

	info.PublicIP = publicIP()
	if upnp {
		m.update(func(s *Status) { s.Net = info; s.Net.UPnP = "trying" })
		pm, ext, err := addPortMapping(port, info.LanIP)
		if err != nil {
			info.UPnP = "failed"
			m.log("[EasyHost] Could not open the port on your router automatically (" + err.Error() + "). Friends on your Wi-Fi can still join; for friends elsewhere see Help.")
		} else {
			info.UPnP = "ok"
			info.RouterIP = ext
			m.mu.Lock()
			if m.cmd != nil {
				m.mapping = pm
				pm = nil
			}
			m.mu.Unlock()
			if pm != nil { // server already gone again
				pm.Remove()
				return
			}
			m.log(fmt.Sprintf("[EasyHost] Opened port %d on your router.", port))
			if ext != "" && info.PublicIP != "" && ext != info.PublicIP {
				info.CGNAT = true
				m.log("[EasyHost] Your internet provider shares one public IP between many customers (CGNAT), so friends outside your home cannot connect directly. A free tunnel like playit.gg solves this.")
			}
		}
	} else {
		info.UPnP = "off"
	}
	m.update(func(s *Status) {
		if s.State == StateStarting || s.State == StateRunning || s.State == StatePreparing {
			s.Net = info
		}
	})
}

func (m *Manager) cleanupOldJars(keep string) {
	entries, _ := os.ReadDir(m.serverDir)
	for _, e := range entries {
		n := e.Name()
		if n != keep && strings.HasSuffix(n, ".jar") && (strings.HasPrefix(n, "paper-") || strings.HasPrefix(n, "server-")) {
			os.Remove(filepath.Join(m.serverDir, n))
		}
	}
}

// Backup zips the world folders into dataDir/backups.
func (m *Manager) Backup() (string, error) {
	running := m.Status().State == StateRunning
	if running {
		m.Command("save-off")
		m.Command("save-all flush")
		time.Sleep(5 * time.Second)
		defer m.Command("save-on")
	}
	dir := filepath.Join(m.dataDir, "backups")
	os.MkdirAll(dir, 0o755)
	name := "world-" + time.Now().Format("2006-01-02_15-04-05") + ".zip"
	out := filepath.Join(dir, name)
	f, err := os.Create(out)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(f)
	count := 0
	entries, _ := os.ReadDir(m.serverDir)
	for _, e := range entries {
		if !e.IsDir() || !isWorldDir(filepath.Join(m.serverDir, e.Name())) {
			continue
		}
		root := filepath.Join(m.serverDir, e.Name())
		filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || info.Name() == "session.lock" {
				return nil
			}
			rel, _ := filepath.Rel(m.serverDir, p)
			w, err := zw.Create(filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			src, err := os.Open(p)
			if err != nil {
				return nil
			}
			io.Copy(w, src)
			src.Close()
			count++
			return nil
		})
	}
	zw.Close()
	f.Close()
	if count == 0 {
		os.Remove(out)
		return "", errors.New("there is no world yet; start the server once first")
	}
	m.log("[EasyHost] Backup saved: " + out)
	return out, nil
}

func isWorldDir(p string) bool {
	_, err := os.Stat(filepath.Join(p, "level.dat"))
	return err == nil
}

func worldExists(serverDir string) bool { return isWorldDir(filepath.Join(serverDir, "world")) }

func lastVersion(recordPath string) string {
	var rec buildRecord
	if b, err := os.ReadFile(recordPath); err == nil && json.Unmarshal(b, &rec) == nil {
		return rec.Version
	}
	return ""
}

// compareVersions compares dotted numeric versions like 1.21.4 and 26.3.
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			fmt.Sscanf(pa[i], "%d", &x)
		}
		if i < len(pb) {
			fmt.Sscanf(pb[i], "%d", &y)
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func typeLabel(t string) string {
	if t == "paper" {
		return "Paper"
	}
	return "Minecraft"
}

// autoMemoryMB picks a heap size that leaves the rest of the PC usable.
func autoMemoryMB() int {
	total := totalMemoryMB()
	if total <= 0 {
		return 2048
	}
	mem := total * 2 / 5 // 40 %
	if mem > 8192 {
		mem = 8192 // more rarely helps a small server and makes GC pauses longer
	}
	if total-mem < 3072 {
		mem = total - 3072
	}
	if mem < 1024 {
		mem = 1024
	}
	return mem / 256 * 256
}

// aikarFlags are the widely used G1 tuning flags recommended by PaperMC.
func aikarFlags(mem int) []string {
	newSize, maxNew, region, reserve, ihop := "30", "40", "8M", "20", "15"
	if mem >= 12*1024 {
		newSize, maxNew, region, reserve, ihop = "40", "50", "16M", "15", "20"
	}
	return []string{
		"-XX:+UseG1GC", "-XX:+ParallelRefProcEnabled", "-XX:MaxGCPauseMillis=200",
		"-XX:+UnlockExperimentalVMOptions", "-XX:+DisableExplicitGC", "-XX:+AlwaysPreTouch",
		"-XX:G1NewSizePercent=" + newSize, "-XX:G1MaxNewSizePercent=" + maxNew,
		"-XX:G1HeapRegionSize=" + region, "-XX:G1ReservePercent=" + reserve,
		"-XX:G1HeapWastePercent=5", "-XX:G1MixedGCCountTarget=4",
		"-XX:InitiatingHeapOccupancyPercent=" + ihop, "-XX:G1MixedGCLiveThresholdPercent=90",
		"-XX:G1RSetUpdatingPauseIntervalMillis=100", "-XX:SurvivorRatio=32",
		"-XX:+PerfDisableSharedMem", "-XX:MaxTenuringThreshold=1",
		"-Dusing.aikars.flags=https://mcflags.emc.gs", "-Daikars.new.flags=true",
	}
}
