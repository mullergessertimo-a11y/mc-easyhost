package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOfflineUUID(t *testing.T) {
	// Known value: Java's UUID.nameUUIDFromBytes("OfflinePlayer:Notch").
	if got := offlineUUID("Notch"); got != "b50ad385-829d-3141-a216-7e7d7539ba7f" {
		t.Errorf("offlineUUID(Notch) = %s", got)
	}
}

func TestListsWhileStopped(t *testing.T) {
	m := testManager(t)
	c := m.cfg.Get()
	c.OnlineMode = false // no network needed
	m.cfg.Set(c)

	if err := m.ChangeList("whitelist", "add", "Steve", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.ChangeList("ops", "add", "Alex", ""); err != nil {
		t.Fatal(err)
	}
	if err := m.ChangeList("bans", "add", "Griefer", "broke stuff"); err != nil {
		t.Fatal(err)
	}
	m.ChangeList("whitelist", "add", "steve", "") // duplicate, other case
	lists, err := m.PlayerLists()
	if err != nil {
		t.Fatal(err)
	}
	if len(lists["whitelist"]) != 1 || lists["whitelist"][0].UUID != offlineUUID("Steve") {
		t.Errorf("whitelist: %+v", lists["whitelist"])
	}
	if len(lists["ops"]) != 1 || len(lists["bans"]) != 1 || lists["bans"][0].Reason != "broke stuff" {
		t.Errorf("lists: %+v", lists)
	}
	b, _ := os.ReadFile(filepath.Join(m.serverDir, "ops.json"))
	if !strings.Contains(string(b), `"level": 4`) {
		t.Errorf("ops.json: %s", b)
	}
	if err := m.ChangeList("bans", "remove", "Griefer", ""); err != nil {
		t.Fatal(err)
	}
	if lists, _ = m.PlayerLists(); len(lists["bans"]) != 0 {
		t.Errorf("ban not removed: %+v", lists["bans"])
	}
	if err := m.ChangeList("ops", "add", "bad name!", ""); err == nil {
		t.Error("accepted invalid name")
	}
	if err := m.ChangeList("secrets", "add", "Steve", ""); err == nil {
		t.Error("accepted unknown list")
	}
}

func TestOnlineMojangLookup(t *testing.T) {
	if os.Getenv("EASYHOST_ONLINE") == "" {
		t.Skip("set EASYHOST_ONLINE=1")
	}
	id, name, err := lookupUUID("notch", true)
	if err != nil || id != "069a79f4-44e9-4726-a5be-fca90e38aaf5" || name != "Notch" {
		t.Fatalf("lookup: %s %s %v", id, name, err)
	}
}

func TestPruneAutoBackups(t *testing.T) {
	m := testManager(t)
	os.MkdirAll(m.backupDir(), 0o755)
	for _, n := range []string{"auto-world-2026-01-01_00-00-00.zip", "auto-world-2026-01-02_00-00-00.zip",
		"auto-world-2026-01-03_00-00-00.zip", "world-2025-01-01_00-00-00.zip"} {
		os.WriteFile(filepath.Join(m.backupDir(), n), nil, 0o644)
	}
	m.pruneAutoBackups(2)
	var names []string
	for _, b := range m.ListBackups() {
		names = append(names, b.Name)
	}
	got := strings.Join(names, ",")
	if strings.Contains(got, "2026-01-01") || !strings.Contains(got, "2026-01-03") || !strings.Contains(got, "world-2025") {
		t.Errorf("after prune: %s", got)
	}
}

func TestAutoRestartLimit(t *testing.T) {
	m := testManager(t)
	for i := 0; i < 3; i++ {
		m.crashes = append(m.crashes, time.Now())
	}
	m.status.State = StateRunning
	m.cmd = nil
	m.onExit(os.ErrClosed) // fourth crash within 15 minutes
	time.Sleep(50 * time.Millisecond)
	if st := m.Status().State; st != StateError {
		t.Fatalf("state %s", st)
	}
	if n := len(m.crashes); n != 4 {
		t.Fatalf("crashes %d", n)
	}
}
