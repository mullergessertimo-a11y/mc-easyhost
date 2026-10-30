package main

import (
	"crypto/md5"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Admins (ops.json), the whitelist and bans live in JSON files next to the
// server. While it runs we change them with commands (the server would
// overwrite file edits); while it is stopped we edit the files, so lists can
// be prepared before anyone joins.

type ListEntry struct {
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
	Reason  string `json:"reason,omitempty"`
	Created string `json:"created,omitempty"`
}

var listFiles = map[string]string{
	"ops":       "ops.json",
	"whitelist": "whitelist.json",
	"bans":      "banned-players.json",
}

var reMCName = regexp.MustCompile(`^[A-Za-z0-9_]{2,16}$`)

func (m *Manager) readList(kind string) ([]map[string]any, error) {
	file, ok := listFiles[kind]
	if !ok {
		return nil, errors.New("unknown list")
	}
	b, err := os.ReadFile(filepath.Join(m.serverDir, file))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var raw []map[string]any
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", file, err)
	}
	return raw, nil
}

func (m *Manager) writeList(kind string, raw []map[string]any) error {
	if raw == nil {
		raw = []map[string]any{}
	}
	b, _ := json.MarshalIndent(raw, "", "  ")
	os.MkdirAll(m.serverDir, 0o755)
	return os.WriteFile(filepath.Join(m.serverDir, listFiles[kind]), b, 0o644)
}

// PlayerLists returns admins, whitelist and bans.
func (m *Manager) PlayerLists() (map[string][]ListEntry, error) {
	out := map[string][]ListEntry{}
	for kind := range listFiles {
		raw, err := m.readList(kind)
		if err != nil {
			return nil, err
		}
		list := []ListEntry{}
		for _, r := range raw {
			e := ListEntry{}
			e.UUID, _ = r["uuid"].(string)
			e.Name, _ = r["name"].(string)
			e.Reason, _ = r["reason"].(string)
			e.Created, _ = r["created"].(string)
			list = append(list, e)
		}
		out[kind] = list
	}
	return out, nil
}

// ChangeList adds or removes a name on one of the lists.
func (m *Manager) ChangeList(kind, action, name, reason string) error {
	if _, ok := listFiles[kind]; !ok {
		return errors.New("unknown list")
	}
	if !reMCName.MatchString(name) {
		return errors.New("that is not a valid Minecraft name")
	}
	if action != "add" && action != "remove" {
		return errors.New("unknown action")
	}
	reason = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, strings.TrimSpace(reason))

	if st := m.Status().State; st == StateRunning || st == StateStarting {
		cmd := map[string]map[string]string{
			"ops":       {"add": "op " + name, "remove": "deop " + name},
			"whitelist": {"add": "whitelist add " + name, "remove": "whitelist remove " + name},
			"bans":      {"add": strings.TrimSpace("ban " + name + " " + reason), "remove": "pardon " + name},
		}[kind][action]
		return m.Command(cmd)
	}
	if !m.isStopped() {
		return errors.New("wait until the server has finished starting or stopping")
	}

	raw, err := m.readList(kind)
	if err != nil {
		return err
	}
	idx := -1
	for i, r := range raw {
		if n, _ := r["name"].(string); strings.EqualFold(n, name) {
			idx = i
		}
	}
	if action == "remove" {
		if idx < 0 {
			return nil
		}
		raw = append(raw[:idx], raw[idx+1:]...)
		return m.writeList(kind, raw)
	}
	if idx >= 0 {
		return nil
	}
	uuid, realName, err := lookupUUID(name, m.cfg.Get().OnlineMode)
	if err != nil {
		return err
	}
	entry := map[string]any{"uuid": uuid, "name": realName}
	switch kind {
	case "ops":
		entry["level"] = 4
		entry["bypassesPlayerLimit"] = false
	case "bans":
		if reason == "" {
			reason = "Banned by an operator."
		}
		entry["created"] = time.Now().Format("2006-01-02 15:04:05 -0700")
		entry["source"] = "MC EasyHost"
		entry["expires"] = "forever"
		entry["reason"] = reason
	}
	return m.writeList(kind, append(raw, entry))
}

// lookupUUID finds the account id the server will see for a name: the
// Mojang account id in online mode, the name-derived one in offline mode.
func lookupUUID(name string, online bool) (string, string, error) {
	if !online {
		return offlineUUID(name), name, nil
	}
	var p struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := getJSON("https://api.mojang.com/users/profiles/minecraft/"+name, &p); err != nil || len(p.ID) != 32 {
		return "", "", fmt.Errorf("no Minecraft account is called %q (or Mojang can't be reached)", name)
	}
	id := p.ID
	return id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:], p.Name, nil
}

// offlineUUID matches Java's UUID.nameUUIDFromBytes("OfflinePlayer:"+name).
func offlineUUID(name string) string {
	h := md5.Sum([]byte("OfflinePlayer:" + name))
	h[6] = h[6]&0x0f | 0x30
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
