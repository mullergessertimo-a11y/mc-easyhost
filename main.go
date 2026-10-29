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
	"strings"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

// A fixed port lets a second launch find the running instance.
const uiPort = 47615

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	headless := flag.Bool("headless", false, "don't open a window and keep running until stopped (for servers without a desktop)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("MC EasyHost", version)
		return
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
	cfg := loadConfig(dataDir)
	hub := NewHub()
	if *headless {
		hub.echo = func(line string) { fmt.Println(line) }
	}
	mgr := NewManager(dataDir, cfg, hub)
	quit := make(chan struct{}, 1)

	srv := &http.Server{Handler: api(mgr, cfg, hub, dataDir, quit)}
	go srv.Serve(ln)
	log.Println("UI on", url, "data in", dataDir)

	if *headless {
		fmt.Printf("MC EasyHost %s running. Control panel: %s\n", version, url)
		fmt.Printf("From another computer: ssh -L %d:127.0.0.1:%d <this-host>, then open %s\n", uiPort, uiPort, url)
	} else if os.Getenv("EASYHOST_NO_WINDOW") == "" {
		openAppWindow(url, filepath.Join(dataDir, "ui-profile"))
	}

	// The app lives as long as a window is open. A short grace period
	// covers page reloads and the browser reconnecting after sleep.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	tick := time.NewTicker(2 * time.Second)
	for running := true; running; {
		select {
		case <-sig:
			running = false
		case <-quit:
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
	mgr.StopAndWait()
}

func api(mgr *Manager, cfg *ConfigStore, hub *Hub, dataDir string, quit chan struct{}) http.Handler {
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
		// EULA consent is only changed through /api/start.
		c.EulaAccepted = cfg.Get().EulaAccepted
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
