package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Config is everything the user can change. Defaults are chosen so that
// pressing "Start" without touching anything gives a working server.
type Config struct {
	ServerType   string `json:"serverType"`   // "paper", "vanilla" or "fabric"
	Version      string `json:"version"`      // "latest" or a concrete version id
	MemoryMB     int    `json:"memoryMB"`     // 0 = automatic
	Port         int    `json:"port"`         // default 25565
	MaxPlayers   int    `json:"maxPlayers"`   // default 10
	Gamemode     string `json:"gamemode"`     // survival, creative, adventure, spectator
	Difficulty   string `json:"difficulty"`   // peaceful, easy, normal, hard
	MOTD         string `json:"motd"`         // server list message
	OnlineMode   bool   `json:"onlineMode"`   // verify accounts with Mojang
	PVP          bool   `json:"pvp"`          // players can hurt each other
	Whitelist    bool   `json:"whitelist"`    // only whitelisted players can join
	UPnP         bool   `json:"upnp"`         // try to open the port on the router
	EulaAccepted bool   `json:"eulaAccepted"` // set only by the user ticking the box
	WorldSeed    string `json:"worldSeed"`    // used only when a new world is generated
	WorldType    string `json:"worldType"`    // normal, flat, large_biomes, amplified
}

func defaultConfig() Config {
	return Config{
		ServerType: "paper",
		Version:    "latest",
		Port:       25565,
		MaxPlayers: 10,
		Gamemode:   "survival",
		Difficulty: "normal",
		MOTD:       "A Minecraft server hosted with MC EasyHost",
		OnlineMode: true,
		PVP:        true,
		UPnP:       true,
	}
}

type ConfigStore struct {
	mu   sync.Mutex
	path string
	cfg  Config
}

func loadConfig(dir string) *ConfigStore {
	s := &ConfigStore{path: filepath.Join(dir, "config.json"), cfg: defaultConfig()}
	if b, err := os.ReadFile(s.path); err == nil {
		_ = json.Unmarshal(b, &s.cfg)
	}
	s.cfg.sanitize()
	return s
}

func (c *Config) sanitize() {
	d := defaultConfig()
	if c.ServerType != "paper" && c.ServerType != "vanilla" && c.ServerType != "fabric" {
		c.ServerType = d.ServerType
	}
	if c.Version == "" {
		c.Version = d.Version
	}
	if c.Port < 1024 || c.Port > 65535 {
		c.Port = d.Port
	}
	if c.MaxPlayers < 1 || c.MaxPlayers > 1000 {
		c.MaxPlayers = d.MaxPlayers
	}
	if c.MemoryMB != 0 && c.MemoryMB < 1024 {
		c.MemoryMB = 1024
	}
	switch c.Gamemode {
	case "survival", "creative", "adventure", "spectator":
	default:
		c.Gamemode = d.Gamemode
	}
	switch c.WorldType {
	case "normal", "flat", "large_biomes", "amplified":
	default:
		c.WorldType = "normal"
	}
	switch c.Difficulty {
	case "peaceful", "easy", "normal", "hard":
	default:
		c.Difficulty = d.Difficulty
	}
}

func (s *ConfigStore) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg
}

func (s *ConfigStore) Set(c Config) error {
	c.sanitize()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = c
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(s.path, b, 0o644)
}
