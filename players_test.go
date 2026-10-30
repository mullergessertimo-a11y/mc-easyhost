package main

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestParsePlayerLine(t *testing.T) {
	cases := []struct {
		line  string
		name  string
		delta int
	}{
		{"[14:02:11 INFO]: Steve joined the game", "Steve", 1},
		{"[14:02:11] [Server thread/INFO]: Alex_99 joined the game", "Alex_99", 1},
		{"\x1b[33;1m[14:02:11 INFO]: \x1b[93mSteve joined the game\x1b[m", "Steve", 1},
		{"[14:02:11 INFO]: §eSteve joined the game", "Steve", 1},
		{"[14:02:11 INFO]: \xa7eSteve joined the game", "Steve", 1}, // § in the Windows code page
		{"[14:02:11 INFO]: \x1b[38;2;255;255;85mSteve joined the game\x1b[0m", "Steve", 1},
		{"[14:02:11 INFO]: Steve (formerly known as Bob) joined the game", "Steve", 1},
		{"[14:02:11 INFO]: Steve[/192.168.178.20:51234] logged in with entity id 123 at ([world]1.5, 64.0, 2.5)", "Steve", 1},
		{"[14:02:11 INFO]: .BedrockGuy[/10.0.0.5:4000] logged in with entity id 7 at (0, 64, 0)", ".BedrockGuy", 1},
		{"[14:05:00 INFO]: Steve lost connection: Disconnected", "Steve", -1},
		{"[14:05:00 INFO]: Steve left the game", "Steve", -1},
		{"[14:05:00 INFO]: §eSteve left the game", "Steve", -1},
		// Things that are not players coming or going:
		{"[14:05:00 INFO]: /127.0.0.1:5555 lost connection: Disconnected", "", 0},
		{"[14:05:00 INFO]: <Alex> haha Bob joined the game", "", 0},
		{"[14:05:00 INFO]: [Not Secure] <Alex> Bob left the game", "", 0},
		{"[14:05:00 INFO]: UUID of player Steve is 069a79f4-44e9-4726-a5be-fca90e38aaf5", "", 0},
		{"[14:05:00 INFO]: Done (3.2s)! For help, type \"help\"", "", 0},
	}
	for _, c := range cases {
		name, delta := parsePlayerLine(cleanLine(c.line))
		if name != c.name || delta != c.delta {
			t.Errorf("%q: got (%q,%d) want (%q,%d)", c.line, name, delta, c.name, c.delta)
		}
	}
}

// fakeMinecraft answers one status request the way a real server does.
func fakeMinecraft(t *testing.T, statusJSON string) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				r := bufio.NewReader(c)
				for i := 0; i < 2; i++ { // handshake, then status request
					n, err := readVarInt(r)
					if err != nil {
						return
					}
					io.CopyN(io.Discard, r, int64(n))
				}
				var body bytes.Buffer
				writeVarInt(&body, 0)
				writeVarInt(&body, int32(len(statusJSON)))
				body.WriteString(statusJSON)
				writePacket(c, body.Bytes())
			}(c)
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestPingServer(t *testing.T) {
	port := fakeMinecraft(t, `{"version":{"name":"Paper 26.3","protocol":800},"players":{"max":10,"online":2,"sample":[{"name":"Steve","id":"a"},{"name":"Alex","id":"b"}]},"description":"hi"}`)
	res, err := pingServer("127.0.0.1", port, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if res.Online != 2 || res.Max != 10 || len(res.Sample) != 2 || res.Sample[0] != "Steve" {
		t.Fatalf("got %+v", res)
	}

	// Servers with hide-online-players send a placeholder name.
	port = fakeMinecraft(t, `{"players":{"max":20,"online":3,"sample":[{"name":"Anonymous Player","id":"0"}]}}`)
	res, err = pingServer("127.0.0.1", port, 2*time.Second)
	if err != nil || res.Online != 3 || len(res.Sample) != 0 {
		t.Fatalf("hidden players: %+v %v", res, err)
	}
}

func TestWatchPlayersUsesPing(t *testing.T) {
	port := fakeMinecraft(t, `{"players":{"max":10,"online":1,"sample":[{"name":"Steve","id":"a"}]}}`)
	m := NewManager(t.TempDir(), loadConfig(t.TempDir()), NewHub())
	m.status.State = StateRunning
	m.players["Ghost"] = true // a leave line we "missed"
	exited := make(chan struct{})
	go m.watchPlayers(port, exited)
	defer close(exited)

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		s := m.Status()
		if s.Count == 1 && len(s.Players) == 1 && s.Players[0] == "Steve" && s.Max == 10 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("status never corrected: %+v", m.Status())
}

func TestVarIntRoundTrip(t *testing.T) {
	for _, v := range []int32{0, 1, 127, 128, 255, 25565, 2097151, -1} {
		var b bytes.Buffer
		writeVarInt(&b, v)
		got, err := readVarInt(bufio.NewReader(&b))
		if err != nil || got != v {
			t.Errorf("varint %d -> %d (%v), bytes %s", v, got, err, strconv.Quote(b.String()))
		}
	}
}
