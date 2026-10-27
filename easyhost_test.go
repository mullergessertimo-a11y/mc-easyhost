package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.21.4", "1.21.10", -1}, {"26.3", "1.21.11", 1}, {"1.20", "1.20.0", 0}, {"26.1.2", "26.1.2", 0},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestLtsJava(t *testing.T) {
	for in, want := range map[int]int{8: 8, 16: 17, 17: 17, 21: 21, 22: 25, 25: 25} {
		if got := ltsJava(in); got != want {
			t.Errorf("ltsJava(%d)=%d want %d", in, got, want)
		}
	}
}

func TestServerPropertiesKeepsUnmanagedKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "server.properties")
	os.WriteFile(p, []byte("#comment\nview-distance=12\nmax-players=3\nmotd=old\n"), 0o644)
	c := defaultConfig()
	c.MOTD = "Grüße"
	c.MaxPlayers = 7
	if err := writeServerProperties(p, c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	s := string(b)
	for _, want := range []string{"#comment", "view-distance=12", "max-players=7", "motd=Gr\\u00FC\\u00DFe", "server-port=25565", "online-mode=true"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in:\n%s", want, s)
		}
	}
	if strings.Count(s, "max-players=") != 1 {
		t.Errorf("duplicate key:\n%s", s)
	}
}

func TestAutoMemory(t *testing.T) {
	m := autoMemoryMB()
	if m < 1024 || m > 8192 || m%256 != 0 {
		t.Errorf("autoMemoryMB()=%d", m)
	}
	t.Logf("total %d MB -> auto %d MB", totalMemoryMB(), m)
}

func TestConfigSanitize(t *testing.T) {
	c := Config{ServerType: "forge", Port: 80, MaxPlayers: -1, Gamemode: "god", MemoryMB: 100}
	c.sanitize()
	if c.ServerType != "paper" || c.Port != 25565 || c.MaxPlayers != 10 || c.Gamemode != "survival" || c.MemoryMB != 1024 {
		t.Errorf("sanitize: %+v", c)
	}
}

// Online test: EASYHOST_ONLINE=1 go test -run Online -v
func TestOnlineResolveAndJava(t *testing.T) {
	if os.Getenv("EASYHOST_ONLINE") == "" {
		t.Skip("set EASYHOST_ONLINE=1")
	}
	for _, typ := range []string{"paper", "vanilla"} {
		b, err := resolveBuild(typ, "latest")
		if err != nil {
			t.Fatal(typ, err)
		}
		t.Logf("%s latest: %s java %d %s", typ, b.Version, b.JavaMajor, b.URL)
		list, err := releaseVersions(typ)
		if err != nil || len(list) < 10 {
			t.Fatalf("%s versions: %v %d", typ, err, len(list))
		}
		t.Logf("%s offers %d versions, newest %s, oldest %s", typ, len(list), list[0], list[len(list)-1])
	}
	old, err := resolveBuild("vanilla", "1.16.5")
	if err != nil || old.JavaMajor != 8 {
		t.Fatalf("1.16.5: %v %+v", err, old)
	}

	dir := os.Getenv("EASYHOST_TEST_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	b, _ := resolveBuild("paper", "latest")
	java, err := ensureJava(dir, b.JavaMajor, func(s string, p float64) {})
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(java, "-version").CombinedOutput()
	if err != nil {
		t.Fatal(err, string(out))
	}
	t.Logf("java ok: %s", strings.SplitN(string(out), "\n", 2)[0])

	jar := filepath.Join(dir, "server", b.JarName)
	if err := downloadFile(b.URL, jar, b.HashAlgo, b.Hash, nil); err != nil {
		t.Fatal(err)
	}
	if err := downloadFile(b.URL, jar+".bad", b.HashAlgo, strings.Repeat("0", 64), nil); err == nil {
		t.Fatal("bad checksum accepted")
	}
	t.Logf("jar verified: %s", jar)
}
