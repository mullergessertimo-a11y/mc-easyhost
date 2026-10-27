# MC EasyHost

One-click Minecraft Java server for Windows. Double-click `MC-EasyHost.exe` and press **Start Server**.

**Download:** grab `MC-EasyHost.exe` from the [Releases](../../releases) page, or the `MC-EasyHost` artifact of the latest [build](../../actions) run.

What it does for you:

- Picks the newest Minecraft version (or one you choose), **Paper** (faster, plugins) or **Vanilla**
- Downloads the right **Java** version privately (Temurin from Adoptium), so nothing gets installed system-wide
- Downloads the server jar and checks its SHA checksum
- Asks once for the Minecraft EULA, then writes `eula.txt` and `server.properties`
- Picks RAM automatically (40% of the PC's memory, between 1 and 8 GB, leaving at least 3 GB for Windows)
- Adds a Windows Firewall rule (one UAC prompt) and opens the port on the router via **UPnP**
- Shows the LAN and internet addresses to share, and warns when the provider uses CGNAT
- Live console, command input, player list (op/kick), whitelist, world backups (zip)
- Refuses to start a world on an older version than it was last played on, since that would corrupt it
- Closing the window saves the world and stops the server. A Windows job object makes sure Java never outlives the app.

Data lives in `%APPDATA%\MC EasyHost` (`server\` holds the world, `backups\`, `java\`).

## How it works

A single Go binary with no runtime dependencies. It serves the UI from `web/` on `127.0.0.1:47615` and opens it
in a chromeless Microsoft Edge window (`--app`), which every Windows 10/11 PC has. If Edge is missing, it falls back
to the default browser. The local API only accepts requests with the right `Host` header and an `X-EasyHost` header,
so websites open in your browser can't control the server.

| File | Purpose |
| --- | --- |
| `main.go` | Startup, single instance, HTTP API, window lifecycle |
| `server.go` | Start pipeline, Java process, console parsing, stop, backups |
| `versions.go` | Mojang version manifest + PaperMC Fill v3 API |
| `java.go` | Downloads Temurin JRE matching the version's required Java |
| `network.go` | LAN/public IP, UPnP port mapping |
| `platform_windows.go` | Job object, hidden console, Edge window, firewall, RAM |
| `web/index.html` | The whole UI |

## Build

```bash
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o build/MC-EasyHost.exe .
```

Tests (the online test downloads Java and a server jar):

```bash
go test ./...
EASYHOST_ONLINE=1 go test -run Online -v ./...
```

For development on Linux: `EASYHOST_NO_WINDOW=1 EASYHOST_DATA=/tmp/eh go run .` then open http://127.0.0.1:47615.

Pushing a tag like `v1.0.0` makes GitHub Actions build the exe and attach it to a release.

## License

MIT. Minecraft is a trademark of Mojang/Microsoft; this project is not affiliated with them. Running a server requires agreeing to the [Minecraft EULA](https://aka.ms/MinecraftEULA).
