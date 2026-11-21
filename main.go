package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

// A fixed port lets a second launch find the running instance.
// EASYHOST_PORT moves it, so a development copy can run next to the real one.
var uiPort = 47615

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	headless := flag.Bool("headless", false, "don't open a window and keep running until stopped (for servers without a desktop)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	afterUpdate := flag.Bool("after-update", false, "started by the updater: wait for the old version to exit and reuse its window")
	resume := flag.Bool("resume", false, "started by the updater: start the server again")
	flag.Parse()
	if *showVersion {
		fmt.Println("MC EasyHost", version)
		return
	}

	if p, err := strconv.Atoi(os.Getenv("EASYHOST_PORT")); err == nil && p > 1024 && p < 65536 {
		uiPort = p
	}

	dataDir := defaultDataDir()
	if d := os.Getenv("EASYHOST_DATA"); d != "" {
		dataDir = d
	}
	os.MkdirAll(dataDir, 0o755)
	var logOut io.Writer = io.Discard
	if lf, err := os.OpenFile(filepath.Join(dataDir, "easyhost.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		logOut = lf
	}
	if *headless {
		logOut = io.MultiWriter(os.Stdout, logOut)
	}
	log.SetOutput(logOut)

	addr := fmt.Sprintf("127.0.0.1:%d", uiPort)
	url := "http://" + addr + "/"
	ln, err := net.Listen("tcp", addr)
	for i := 0; err != nil && *afterUpdate && i < 60; i++ {
		time.Sleep(500 * time.Millisecond) // the old version is still shutting down
		ln, err = net.Listen("tcp", addr)
	}
	if err != nil {
		if *headless {
			fmt.Fprintln(os.Stderr, "MC EasyHost is already running (port", uiPort, "is in use)")
			os.Exit(1)
		}
		// Already running: just bring up another window to it.
		openAppWindow(url, filepath.Join(dataDir, "ui-profile"))
		return
	}

	initPlatform()
	go cleanupOldBinary()
	cfg := loadConfig(dataDir)
	hub := NewHub()
	if *headless {
		hub.echo = func(line string) { fmt.Println(line) }
	}
	mgr := NewManager(dataDir, cfg, hub)
	quit := make(chan struct{}, 1)
	restart := make(chan string, 1)
	updater := &Updater{}
	if version != "dev" {
		go updater.Check(false)
	}

	srv := &http.Server{Handler: api(mgr, cfg, hub, dataDir, quit, updater, restart)}
	go srv.Serve(ln)
	log.Println("UI on", url, "data in", dataDir)

	if *headless {
		fmt.Printf("MC EasyHost %s running. Control panel: %s\n", version, url)
		fmt.Printf("From another computer: ssh -L %d:127.0.0.1:%d <this-host>, then open %s\n", uiPort, uiPort, url)
	} else if os.Getenv("EASYHOST_NO_WINDOW") == "" && !*afterUpdate {
		openAppWindow(url, filepath.Join(dataDir, "ui-profile"))
	}

	if *resume {
		mgr.log("[EasyHost] Updated to " + version + ". Starting the server again.")
		if err := mgr.Start(); err != nil {
			mgr.log("[EasyHost] Could not start the server: " + err.Error())
		}
	} else if *afterUpdate {
		mgr.log("[EasyHost] Updated to " + version + ".")
	}

	// The app lives as long as a window is open. A short grace period
	// covers page reloads and the browser reconnecting after sleep.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	tick := time.NewTicker(2 * time.Second)
	restartTo := ""
	for running := true; running; {
		select {
		case <-sig:
			running = false
		case <-quit:
			running = false
		case restartTo = <-restart:
			running = false
		case <-tick.C:
			if *headless {
				continue // runs until SIGTERM / Ctrl+C / Quit
			}
			idle := hub.IdleFor()
			if (hub.HadClient() && idle > 20*time.Second) || (!hub.HadClient() && idle > 3*time.Minute) {
				running = false
			}
		}
	}
	log.Println("shutting down")
	wasLive := mgr.Status().State == StateRunning || mgr.Status().State == StateStarting
	mgr.StopAndWait()
	mgr.stopTunnel()
	if restartTo != "" {
		hub.SendLog("[EasyHost] Restarting into the new version…")
		srv.Close()
		args := []string{}
		for _, a := range os.Args[1:] {
			if a != "--after-update" && a != "-after-update" && a != "--resume" && a != "-resume" {
				args = append(args, a)
			}
		}
		args = append(args, "--after-update")
		if wasLive {
			args = append(args, "--resume")
		}
		if err := restartSelf(restartTo, args); err != nil {
			log.Println("restart after update failed:", err)
		}
	}
}

func api(mgr *Manager, cfg *ConfigStore, hub *Hub, dataDir string, quit chan struct{}, updater *Updater, restart chan string) http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(static)))

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"status": mgr.Status(),
			"config": cfg.Get(),
			"info": map[string]any{
				"dataDir":  dataDir,
				"totalMB":  totalMemoryMB(),
				"autoMB":   autoMemoryMB(),
				"hasWorld": worldExists(mgr.serverDir),
				"os":       runtime.GOOS,
				"version":  version,
			},
		})
	})

	mux.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		c, backlog := hub.Subscribe()
		defer hub.Unsubscribe(c)
		w.Write(event("backlog", backlog))
		w.Write(event("status", mgr.Status()))
		fl.Flush()
		ping := time.NewTicker(15 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case msg := <-c:
				w.Write(msg)
				fl.Flush()
			case <-ping.C:
				w.Write([]byte(": ping\n\n"))
				fl.Flush()
			}
		}
	})

	mux.HandleFunc("GET /api/versions", func(w http.ResponseWriter, r *http.Request) {
		list, err := releaseVersions(r.URL.Query().Get("type"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, list)
	})

	mux.HandleFunc("POST /api/config", func(w http.ResponseWriter, r *http.Request) {
		var c Config
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeErr(w, err)
			return
		}
		// EULA consent is only changed through /api/start, the tunnel
		// through /api/tunnel. A modpack's Fabric loader stays only while
		// its Minecraft version stays.
		old := cfg.Get()
		c.EulaAccepted, c.Tunnel = old.EulaAccepted, old.Tunnel
		c.FabricLoader = ""
		if c.ServerType == old.ServerType && c.Version == old.Version {
			c.FabricLoader = old.FabricLoader
		}
		if err := cfg.Set(c); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, cfg.Get())
	})

	mux.HandleFunc("POST /api/start", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AcceptEula bool `json:"acceptEula"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.AcceptEula {
			c := cfg.Get()
			c.EulaAccepted = true
			cfg.Set(c)
		}
		if err := mgr.Start(); err != nil {
			if errors.Is(err, ErrEula) {
				w.WriteHeader(http.StatusPreconditionRequired)
				writeJSON(w, map[string]string{"error": "eula"})
				return
			}
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/stop", func(w http.ResponseWriter, r *http.Request) {
		mgr.Stop()
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/command", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Command string `json:"command"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if err := mgr.Command(body.Command); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/backup", func(w http.ResponseWriter, r *http.Request) {
		path, err := mgr.Backup()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]string{"path": path})
	})

	mux.HandleFunc("POST /api/open", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			What string `json:"what"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		dir := mgr.serverDir
		if body.What == "backups" {
			dir = filepath.Join(dataDir, "backups")
		}
		os.MkdirAll(dir, 0o755)
		openFolder(dir)
		writeJSON(w, map[string]bool{"ok": true})
	})

	// ---- admins, whitelist, bans ----
	mux.HandleFunc("GET /api/lists", func(w http.ResponseWriter, r *http.Request) {
		lists, err := mgr.PlayerLists()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, lists)
	})
	mux.HandleFunc("POST /api/lists", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Kind, Action, Name, Reason string
		}
		json.NewDecoder(r.Body).Decode(&body)
		if err := mgr.ChangeList(body.Kind, body.Action, body.Name, body.Reason); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	// ---- mods / plugins ----
	mux.HandleFunc("GET /api/mods", func(w http.ResponseWriter, r *http.Request) {
		list, folder, err := mgr.ListMods()
		res := map[string]any{"type": cfg.Get().ServerType, "folder": folder, "installed": list}
		if err != nil {
			res["error"] = err.Error()
		}
		if v, verr := mgr.targetVersion(); verr == nil {
			res["version"] = v
		}
		res["crossplay"] = mgr.crossplayInstalled(cfg.Get().ServerType)
		res["bedrockPort"] = mgr.bedrockPort(cfg.Get().ServerType)
		res["modpack"] = mgr.CurrentModpack()
		writeJSON(w, res)
	})
	mux.HandleFunc("POST /api/crossplay", func(w http.ResponseWriter, r *http.Request) {
		installed, err := mgr.EnableCrossplay()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"installed": installed})
	})

	// ---- modpacks ----
	mux.HandleFunc("GET /api/modpacks/search", func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		hits, total, err := mgr.SearchModpacks(r.URL.Query().Get("q"), offset)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"hits": hits, "total": total})
	})
	mux.HandleFunc("POST /api/modpacks/install", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID string `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		p, err := mgr.InstallModpack(body.ID)
		if err != nil {
			mgr.log("[EasyHost] Modpack install failed: " + err.Error())
			writeErr(w, err)
			return
		}
		writeJSON(w, p)
	})
	mux.HandleFunc("POST /api/modpacks/upload", func(w http.ResponseWriter, r *http.Request) {
		p, err := mgr.ImportModpack(r.Body)
		if err != nil {
			mgr.log("[EasyHost] Modpack install failed: " + err.Error())
			writeErr(w, err)
			return
		}
		writeJSON(w, p)
	})

	// ---- play from anywhere (playit.gg) ----
	mux.HandleFunc("POST /api/tunnel/link", func(w http.ResponseWriter, r *http.Request) {
		u, err := mgr.TunnelLink()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]string{"url": u})
	})
	mux.HandleFunc("POST /api/tunnel/unlink", func(w http.ResponseWriter, r *http.Request) {
		mgr.TunnelUnlink()
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/tunnel/set", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			On bool `json:"on"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if err := mgr.SetTunnel(body.On); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, cfg.Get())
	})
	mux.HandleFunc("POST /api/tunnel/retry", func(w http.ResponseWriter, r *http.Request) {
		if err := mgr.RetryTunnel(); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})

	// ---- app updates ----
	mux.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, updater.Check(r.URL.Query().Get("force") == "1"))
	})
	mux.HandleFunc("POST /api/update", func(w http.ResponseWriter, r *http.Request) {
		exe, err := updater.Install(dataDir)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		go func() {
			time.Sleep(300 * time.Millisecond)
			restart <- exe
		}()
	})
	mux.HandleFunc("GET /api/mods/search", func(w http.ResponseWriter, r *http.Request) {
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		hits, total, err := mgr.SearchMods(r.URL.Query().Get("q"), offset)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"hits": hits, "total": total})
	})
	mux.HandleFunc("POST /api/mods/install", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID string `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		installed, err := mgr.InstallMod(body.ID)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"installed": installed})
	})
	mux.HandleFunc("POST /api/mods/remove", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			File string `json:"file"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if err := mgr.RemoveMod(body.File); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	})
	mux.HandleFunc("POST /api/mods/update", func(w http.ResponseWriter, r *http.Request) {
		updated, problems := mgr.UpdateMods()
		writeJSON(w, map[string]any{"updated": updated, "problems": problems})
	})

	// ---- worlds ----
	mux.HandleFunc("GET /api/worlds", func(w http.ResponseWriter, r *http.Request) {
		c := cfg.Get()
		writeJSON(w, map[string]any{
			"current":      mgr.CurrentWorld(),
			"singleplayer": SingleplayerWorlds(),
			"backups":      mgr.ListBackups(),
			"seed":         c.WorldSeed,
			"worldType":    c.WorldType,
		})
	})
	worldAction := func(f func(r *http.Request) error) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if err := f(r); err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, map[string]bool{"ok": true})
		}
	}
	mux.HandleFunc("POST /api/worlds/import", worldAction(func(r *http.Request) error {
		var body struct {
			Path string `json:"path"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		return mgr.ImportWorld(body.Path)
	}))
	mux.HandleFunc("POST /api/worlds/upload", worldAction(func(r *http.Request) error {
		return mgr.ImportWorldZip(r.Body)
	}))
	mux.HandleFunc("POST /api/worlds/new", worldAction(func(r *http.Request) error {
		var body struct {
			Seed string `json:"seed"`
			Type string `json:"type"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		return mgr.NewWorld(body.Seed, body.Type)
	}))
	mux.HandleFunc("POST /api/backups/restore", worldAction(func(r *http.Request) error {
		var body struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		return mgr.RestoreBackup(body.Name)
	}))
	mux.HandleFunc("POST /api/backups/delete", worldAction(func(r *http.Request) error {
		var body struct {
			Name string `json:"name"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		return mgr.DeleteBackup(body.Name)
	}))

	mux.HandleFunc("POST /api/quit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]bool{"ok": true})
		select {
		case quit <- struct{}{}:
		default:
		}
	})

	return guard(mux)
}

// guard keeps other websites open in the user's browser from talking to
// this local API: the Host header must be ours (blocks DNS rebinding) and
// every POST must carry a custom header (blocks cross-site form posts,
// since browsers won't send custom headers cross-origin without CORS).
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if host != fmt.Sprintf("127.0.0.1:%d", uiPort) && host != fmt.Sprintf("localhost:%d", uiPort) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != "GET" && r.Header.Get("X-EasyHost") != "1" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
