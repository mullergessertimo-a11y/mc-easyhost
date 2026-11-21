package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// "Play from anywhere" uses a free playit.gg tunnel. It gives the server a
// public address that works behind CGNAT, school/dorm networks and routers
// without UPnP. The user links a playit account once in the browser; after
// that the official playit agent runs next to the server and we create the
// tunnels through playit's API.

var playitAPI = "https://api.playit.gg" // a var so tests can point it elsewhere

// The agent is pinned to a release we tested, with its published checksums.
var playitAgents = map[string]struct{ URL, SHA256 string }{
	"windows/amd64": {"https://github.com/playit-cloud/playit-agent/releases/download/v2.0.2/playitd-windows-x86_64-signed.exe",
		"6e62da9f56a3bcf72e0c62c29e94609c847cc9b110c460f68b20035525985238"},
	"linux/amd64": {"https://github.com/playit-cloud/playit-agent/releases/download/v2.0.2/playit-linux-amd64",
		"9d27a8dbb4d2d8a7107fcabfa33028c789a30a64c97a36447fe4afb94e08dde9"},
	"linux/arm64": {"https://github.com/playit-cloud/playit-agent/releases/download/v2.0.2/playit-linux-aarch64",
		"aed86fc2b82a772ca0e1d5cddec6c3123f96e12a099f7ae45aaa59a9a430a2cd"},
}

type TunnelInfo struct {
	State    string `json:"state"`              // "", "linking", "connecting", "online", "error"
	Linked   bool   `json:"linked"`             // a playit account is connected
	ClaimURL string `json:"claimURL,omitempty"` // page where the user approves the link
	Java     string `json:"java,omitempty"`     // address for Java players
	Bedrock  string `json:"bedrock,omitempty"`  // address for Bedrock players (crossplay)
	Error    string `json:"error,omitempty"`
}

type playitTunnel struct {
	ID             string `json:"id"`
	TunnelType     string `json:"tunnel_type"`
	LocalPort      int    `json:"local_port"`
	AssignedDomain string `json:"assigned_domain"`
	CustomDomain   string `json:"custom_domain"`
	Port           struct {
		From int `json:"from"`
	} `json:"port"`
	Disabled any `json:"disabled"`
}

type playitRunData struct {
	AgentID       string         `json:"agent_id"`
	AccountStatus string         `json:"account_status"`
	Tunnels       []playitTunnel `json:"tunnels"`
	Pending       []struct {
		ID         string `json:"id"`
		TunnelType string `json:"tunnel_type"`
	} `json:"pending"`
}

func (m *Manager) playitDir() string        { return filepath.Join(m.dataDir, "playit") }
func (m *Manager) playitSecretPath() string { return filepath.Join(m.playitDir(), "secret.txt") }

func (m *Manager) playitSecret() string {
	b, err := os.ReadFile(m.playitSecretPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// playitCall posts to playit's API, which answers
// {"status":"success"|"fail"|"error","data":...}.
func playitCall(path, secret string, req, out any) error {
	body, _ := json.Marshal(req)
	r, _ := http.NewRequest("POST", playitAPI+path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("User-Agent", userAgent)
	if secret != "" {
		r.Header.Set("Authorization", "Agent-Key "+secret)
	}
	c := &http.Client{Timeout: 20 * time.Second}
	resp, err := c.Do(r)
	if err != nil {
		return fmt.Errorf("could not reach playit.gg: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var res struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return fmt.Errorf("playit.gg answered %s", resp.Status)
	}
	switch res.Status {
	case "success":
		if out != nil {
			return json.Unmarshal(res.Data, out)
		}
		return nil
	case "fail":
		var code string
		if json.Unmarshal(res.Data, &code) != nil {
			code = string(res.Data)
		}
		return &playitFail{code}
	}
	var e struct {
		Message string `json:"message"`
	}
	json.Unmarshal(res.Data, &e)
	return fmt.Errorf("playit.gg error: %s", e.Message)
}

type playitFail struct{ Code string }

func (e *playitFail) Error() string {
	switch e.Code {
	case "RequiresVerifiedAccount":
		return "playit.gg wants you to confirm your email address first. Check your inbox, then press Retry"
	case "RequiresPlayitPremium":
		return "this needs playit.gg Premium on your account"
	case "AgentNotFound", "InvalidAgentId":
		return "playit.gg doesn't know this app any more. Press Disconnect, then connect again"
	}
	return "playit.gg refused: " + e.Code
}

// TunnelLink starts linking a playit account: the user opens ClaimURL,
// logs in (or creates a free account) and approves "MC EasyHost".
func (m *Manager) TunnelLink() (string, error) {
	if m.playitSecret() != "" {
		return "", errors.New("already connected to playit.gg")
	}
	if _, ok := playitAgents[runtime.GOOS+"/"+runtime.GOARCH]; !ok {
		return "", errors.New("playit.gg is not available for this system yet")
	}
	buf := make([]byte, 5)
	rand.Read(buf)
	code := hex.EncodeToString(buf)
	claimURL := "https://playit.gg/claim/" + code
	stop := make(chan struct{})
	m.mu.Lock()
	if m.claimStop != nil {
		close(m.claimStop)
	}
	m.claimStop = stop
	m.mu.Unlock()
	m.setTunnel(func(t *TunnelInfo) { *t = TunnelInfo{State: "linking", ClaimURL: claimURL} })
	go m.waitForClaim(code, stop)
	return claimURL, nil
}

func (m *Manager) waitForClaim(code string, stop chan struct{}) {
	fail := func(msg string) {
		m.setTunnel(func(t *TunnelInfo) { *t = TunnelInfo{State: "error", Error: msg} })
	}
	deadline := time.Now().Add(20 * time.Minute)
	setup := map[string]string{"code": code, "agent_type": "self-managed", "version": "MC EasyHost " + version}
	for {
		select {
		case <-stop:
			return
		case <-time.After(2 * time.Second):
		}
		if time.Now().After(deadline) {
			fail("Linking timed out. Press Connect to try again.")
			return
		}
		var state string
		if err := playitCall("/claim/setup", "", setup, &state); err != nil {
			continue // offline for a moment; keep waiting
		}
		if state == "UserRejected" {
			fail("The link was declined on playit.gg.")
			return
		}
		if state != "UserAccepted" {
			continue
		}
		var key struct {
			SecretKey string `json:"secret_key"`
		}
		var err error
		for i := 0; i < 10; i++ {
			if err = playitCall("/claim/exchange", "", map[string]string{"code": code}, &key); err == nil && key.SecretKey != "" {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if key.SecretKey == "" {
			fail("Linking failed: " + fmt.Sprint(err))
			return
		}
		os.MkdirAll(m.playitDir(), 0o700)
		if err := os.WriteFile(m.playitSecretPath(), []byte(key.SecretKey+"\n"), 0o600); err != nil {
			fail("Could not save the playit key: " + err.Error())
			return
		}
		c := m.cfg.Get()
		c.Tunnel = true
		m.cfg.Set(c)
		m.log("[EasyHost] Connected to playit.gg.")
		m.setTunnel(func(t *TunnelInfo) { *t = TunnelInfo{Linked: true} })
		if st := m.Status().State; st == StateStarting || st == StateRunning {
			go m.startTunnel(c.Port, m.bedrockPort(c.ServerType))
		}
		return
	}
}

// TunnelUnlink forgets the playit account. Its tunnels are deleted so
// nothing is left pointing at this PC.
func (m *Manager) TunnelUnlink() {
	m.mu.Lock()
	if m.claimStop != nil {
		close(m.claimStop)
		m.claimStop = nil
	}
	m.mu.Unlock()
	m.stopTunnel()
	secret := m.playitSecret()
	if secret != "" {
		var rd playitRunData
		if playitCall("/agents/rundata", secret, map[string]any{}, &rd) == nil {
			for _, t := range rd.Tunnels {
				playitCall("/tunnels/delete", secret, map[string]string{"tunnel_id": t.ID}, nil)
			}
		}
		os.Remove(m.playitSecretPath())
		m.log("[EasyHost] Disconnected from playit.gg.")
	}
	c := m.cfg.Get()
	c.Tunnel = false
	m.cfg.Set(c)
	m.setTunnel(func(t *TunnelInfo) { *t = TunnelInfo{} })
}

// SetTunnel turns the tunnel on or off without unlinking the account.
func (m *Manager) SetTunnel(on bool) error {
	if on && m.playitSecret() == "" {
		return errors.New("connect a playit.gg account first")
	}
	c := m.cfg.Get()
	c.Tunnel = on
	if err := m.cfg.Set(c); err != nil {
		return err
	}
	live := m.Status().State == StateStarting || m.Status().State == StateRunning
	if on && live {
		go m.startTunnel(c.Port, m.bedrockPort(c.ServerType))
	} else if !on {
		m.stopTunnel()
	}
	return nil
}

func (m *Manager) setTunnel(f func(t *TunnelInfo)) {
	m.update(func(s *Status) {
		f(&s.Tunnel)
		if s.Tunnel.State != "linking" && s.Tunnel.State != "error" {
			s.Tunnel.Linked = m.playitSecret() != ""
		}
	})
}

// ensurePlayitAgent downloads the official agent once.
func (m *Manager) ensurePlayitAgent() (string, error) {
	a, ok := playitAgents[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", errors.New("playit.gg is not available for this system")
	}
	name := "playitd"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(m.playitDir(), a.SHA256[:8]+"-"+name)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	m.setTunnel(func(t *TunnelInfo) { t.State = "connecting" })
	m.log("[EasyHost] Downloading the playit.gg agent (one time)")
	if err := downloadFile(a.URL, path, "sha256", a.SHA256, nil); err != nil {
		return "", fmt.Errorf("downloading the playit.gg agent failed: %w", err)
	}
	os.Chmod(path, 0o755)
	// Remove agents of older app versions.
	entries, _ := os.ReadDir(m.playitDir())
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-"+name) && e.Name() != filepath.Base(path) {
			os.Remove(filepath.Join(m.playitDir(), e.Name()))
		}
	}
	return path, nil
}

// ensureTunnels makes sure the account has a Java tunnel to our port (and a
// Bedrock one when crossplay is on) and returns the public addresses once
// playit has assigned them.
func ensureTunnels(secret string, port, bedrockPort int, waitFor time.Duration) (java, bedrock string, err error) {
	var rd playitRunData
	if err := playitCall("/agents/rundata", secret, map[string]any{}, &rd); err != nil {
		return "", "", err
	}
	switch rd.AccountStatus {
	case "email-not-verified":
		return "", "", errors.New("confirm your email address on playit.gg first (check your inbox)")
	case "banned", "agent-disabled", "account-delete-scheduled":
		return "", "", fmt.Errorf("playit.gg has disabled this account (%s)", rd.AccountStatus)
	case "agent-over-limit":
		return "", "", errors.New("your playit.gg account has too many agents. Remove an old one on playit.gg")
	}
	want := []struct {
		typ, proto string
		port       int
	}{{"minecraft-java", "tcp", port}}
	if bedrockPort > 0 {
		want = append(want, struct {
			typ, proto string
			port       int
		}{"minecraft-bedrock", "udp", bedrockPort})
	}
	for _, w := range want {
		found := false
		for _, t := range rd.Tunnels {
			if t.TunnelType != w.typ {
				continue
			}
			found = true
			if t.LocalPort != w.port {
				if err := playitCall("/tunnels/update", secret, map[string]any{
					"tunnel_id": t.ID, "local_ip": "127.0.0.1", "local_port": w.port, "agent_id": rd.AgentID, "enabled": true,
				}, nil); err != nil {
					return "", "", err
				}
			}
			break
		}
		for _, p := range rd.Pending {
			if p.TunnelType == w.typ {
				found = true
			}
		}
		if found {
			continue
		}
		var id struct {
			ID string `json:"id"`
		}
		if err := playitCall("/tunnels/create", secret, map[string]any{
			"name": "MC EasyHost " + strings.TrimPrefix(w.typ, "minecraft-"), "tunnel_type": w.typ,
			"port_type": w.proto, "port_count": 1, "enabled": true,
			"origin": map[string]any{"type": "agent", "data": map[string]any{
				"agent_id": rd.AgentID, "local_ip": "127.0.0.1", "local_port": w.port}},
			"alloc": nil, "firewall_id": nil, "proxy_protocol": nil,
		}, &id); err != nil {
			return "", "", err
		}
	}

	// New tunnels take a few seconds to get an address.
	deadline := time.Now().Add(waitFor)
	for {
		for _, t := range rd.Tunnels {
			domain := t.CustomDomain
			if domain == "" {
				domain = t.AssignedDomain
			}
			if domain == "" {
				continue
			}
			switch t.TunnelType {
			case "minecraft-java":
				java = domain // joinmc.link names carry an SRV record, no port needed
			case "minecraft-bedrock":
				if bedrockPort > 0 {
					bedrock = domain + ":" + strconv.Itoa(t.Port.From)
				}
			}
		}
		if java != "" && (bedrockPort == 0 || bedrock != "") || time.Now().After(deadline) {
			break
		}
		time.Sleep(3 * time.Second)
		if err := playitCall("/agents/rundata", secret, map[string]any{}, &rd); err != nil {
			return "", "", err
		}
	}
	if java == "" {
		return "", "", errors.New("playit.gg has not assigned an address yet. Press Retry in a minute")
	}
	return java, bedrock, nil
}

// startTunnel runs while the server runs. Failures never stop the server.
func (m *Manager) startTunnel(port, bedrockPort int) {
	secret := m.playitSecret()
	if secret == "" {
		return
	}
	m.mu.Lock()
	if m.tunnelCmd != nil {
		m.mu.Unlock()
		return
	}
	placeholder := &exec.Cmd{} // claims the slot while we set up
	m.tunnelCmd = placeholder
	m.mu.Unlock()
	fail := func(err error) {
		m.mu.Lock()
		if m.tunnelCmd == placeholder {
			m.tunnelCmd = nil
		}
		m.mu.Unlock()
		m.log("[EasyHost] Play from anywhere (playit.gg): " + err.Error())
		m.setTunnel(func(t *TunnelInfo) { t.State = "error"; t.Error = err.Error(); t.Java = ""; t.Bedrock = "" })
	}
	m.setTunnel(func(t *TunnelInfo) { t.State = "connecting"; t.Error = "" })

	agent, err := m.ensurePlayitAgent()
	if err != nil {
		fail(err)
		return
	}
	cmd := exec.Command(agent, "--secret-path", m.playitSecretPath(),
		"--socket-path", playitSocketPath(m.playitDir()),
		"--log-path", filepath.Join(m.playitDir(), "playit.log"))
	cmd.Dir = m.playitDir()
	setupChildProcess(cmd)
	if err := cmd.Start(); err != nil {
		fail(fmt.Errorf("could not start the playit.gg agent: %w", err))
		return
	}
	attachChildProcess(cmd)
	m.mu.Lock()
	if m.tunnelCmd != placeholder { // stopped while we were setting up
		m.mu.Unlock()
		cmd.Process.Kill()
		cmd.Wait()
		return
	}
	m.tunnelCmd = cmd
	m.mu.Unlock()
	go func() {
		cmd.Wait()
		m.mu.Lock()
		mine := m.tunnelCmd == cmd
		if mine {
			m.tunnelCmd = nil
		}
		m.mu.Unlock()
		if mine { // not stopped by us
			m.log("[EasyHost] The playit.gg agent stopped unexpectedly; see " + filepath.Join(m.playitDir(), "playit.log"))
			m.setTunnel(func(t *TunnelInfo) {
				t.State = "error"
				t.Error = "The playit.gg agent stopped. Press Retry."
				t.Java = ""
				t.Bedrock = ""
			})
		}
	}()

	java, bedrock, err := ensureTunnels(secret, port, bedrockPort, 90*time.Second)
	if err != nil {
		m.mu.Lock()
		mine := m.tunnelCmd == cmd
		if mine {
			m.tunnelCmd = nil
		}
		m.mu.Unlock()
		cmd.Process.Kill()
		if mine {
			m.log("[EasyHost] Play from anywhere (playit.gg): " + err.Error())
			m.setTunnel(func(t *TunnelInfo) { t.State = "error"; t.Error = err.Error(); t.Java = ""; t.Bedrock = "" })
		}
		return
	}
	m.mu.Lock()
	still := m.tunnelCmd == cmd
	m.mu.Unlock()
	if !still {
		return // stopped meanwhile, or the agent died and the watcher reported it
	}
	m.log("[EasyHost] Play from anywhere is on. Friends join with: " + java)
	if bedrock != "" {
		m.log("[EasyHost] Bedrock players (phones, consoles) use: " + bedrock)
	}
	m.setTunnel(func(t *TunnelInfo) { t.State = "online"; t.Java = java; t.Bedrock = bedrock; t.Error = "" })
}

func (m *Manager) stopTunnel() {
	m.mu.Lock()
	cmd := m.tunnelCmd
	m.tunnelCmd = nil
	m.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
	m.setTunnel(func(t *TunnelInfo) {
		if t.State != "linking" {
			t.State = ""
			t.Java, t.Bedrock = "", ""
			if t.Error != "" && t.Linked {
				t.Error = ""
			}
		}
	})
}

// RetryTunnel restarts the tunnel after an error.
func (m *Manager) RetryTunnel() error {
	st := m.Status().State
	if st != StateStarting && st != StateRunning {
		return errors.New("start the server first")
	}
	m.stopTunnel()
	c := m.cfg.Get()
	go m.startTunnel(c.Port, m.bedrockPort(c.ServerType))
	return nil
}
