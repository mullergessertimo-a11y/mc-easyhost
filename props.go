package main

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// writeServerProperties updates only the keys MC EasyHost manages and keeps
// every other line (including ones the user edited by hand).
func writeServerProperties(path string, c Config) error {
	managed := map[string]string{
		"server-port":    strconv.Itoa(c.Port),
		"query.port":     strconv.Itoa(c.Port),
		"max-players":    strconv.Itoa(c.MaxPlayers),
		"gamemode":       c.Gamemode,
		"difficulty":     c.Difficulty,
		"motd":           escapeProp(c.MOTD),
		"online-mode":    strconv.FormatBool(c.OnlineMode),
		"pvp":            strconv.FormatBool(c.PVP),
		"white-list":     strconv.FormatBool(c.Whitelist),
		"enforce-whitelist": strconv.FormatBool(c.Whitelist),
	}

	var lines []string
	seen := map[string]bool{}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if k, _, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(strings.TrimSpace(line), "#") {
				k = strings.TrimSpace(k)
				if v, ok := managed[k]; ok {
					line = k + "=" + v
					seen[k] = true
				}
			}
			lines = append(lines, line)
		}
		f.Close()
	}
	for _, k := range []string{"server-port", "query.port", "max-players", "gamemode", "difficulty", "motd", "online-mode", "pvp", "white-list", "enforce-whitelist"} {
		if !seen[k] {
			lines = append(lines, k+"="+managed[k])
		}
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// escapeProp escapes non-ASCII characters the way Java's Properties expects.
func escapeProp(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r > 0x7e || r < 0x20:
			if r > 0xffff {
				b.WriteRune('?')
			} else {
				b.WriteString(`\u` + strings.ToUpper(strconv.FormatInt(int64(r)+0x10000, 16)[1:]))
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
