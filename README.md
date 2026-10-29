# MC EasyHost

One-click Minecraft Java server for **Windows** and **Linux**. Start the app and press **Start Server**.

## Download

Grab the file for your system from the [Releases](../../releases) page:

| System | File |
| --- | --- |
| Windows 10/11 | `MC-EasyHost.exe`: just double-click it |
| Linux (x86-64) | `mc-easyhost-linux-x86_64.tar.gz` |
| Linux (ARM64, e.g. Raspberry Pi 4/5) | `mc-easyhost-linux-aarch64.tar.gz` |

### Linux install

```bash
tar xzf mc-easyhost-linux-x86_64.tar.gz
cd mc-easyhost-linux-x86_64 && ./install.sh
```

This puts `mc-easyhost` in `~/.local/bin` and adds **MC EasyHost** to your app menu. No root is needed, and
`./install.sh --uninstall` removes it again. You can also run `./mc-easyhost` straight from the folder without
installing.

**Headless servers** (no desktop): run `mc-easyhost --headless`. It keeps running until stopped and prints the
console to stdout. Open the control panel through an SSH tunnel:

```bash
ssh -L 47615:127.0.0.1:47615 your-server
```

Then browse to http://127.0.0.1:47615. To start it on boot, use the included `mc-easyhost.service`
(instructions are inside the file).

## What it does for you

- Picks the newest Minecraft version (or one you choose), **Paper** (faster, plugins) or **Vanilla**
- Downloads the right **Java** version privately (Temurin from Adoptium), so nothing gets installed system-wide
- Downloads the server jar and checks its SHA checksum
- Asks once for the Minecraft EULA, then writes `eula.txt` and `server.properties`
- Picks RAM automatically (40% of the PC's memory, between 1 and 8 GB, leaving at least 3 GB for the system)
- Opens the firewall: a Windows Firewall rule (one UAC prompt), or on Linux ufw/firewalld when one is active
  (one password prompt). It also opens the port on the router via **UPnP**, explaining what to change when the
  router refuses.
- Shows the LAN and internet addresses to share, and warns when the provider uses CGNAT
- Live console, command input, player list (op/kick), whitelist, world backups (zip)
- Refuses to start a world on an older version than it was last played on, since that would corrupt it
- Closing the window saves the world and stops the server, and Java never outlives the app. On Windows a job
  object handles this; on Linux the parent-death signal does.

| | Windows | Linux |
| --- | --- | --- |
| Data (world, backups, Java) | `%APPDATA%\MC EasyHost` | `~/.local/share/mc-easyhost` |
| App window | Edge in app mode | Chrome/Chromium/Edge/Brave in app mode, else default browser |
| Firewall | Windows Firewall | ufw or firewalld, if switched on |

## How it works

A single Go binary with no runtime dependencies. It serves the UI from `web/` on `127.0.0.1:47615` and opens it
in a chromeless browser window (`--app`). The local API only accepts requests with the right `Host` header and an
`X-EasyHost` header, so websites open in your browser can't control the server.

| File | Purpose |
| --- | --- |
| `main.go` | Startup, flags, single instance, HTTP API, window lifecycle |
| `server.go` | Start pipeline, Java process, console parsing, stop, backups |
| `versions.go` | Mojang version manifest + PaperMC Fill v3 API |
| `java.go` | Downloads Temurin JRE matching the version's required Java |
| `network.go` | LAN/public IP, UPnP port mapping and router error hints |
| `platform_windows.go` | Job object, hidden console, Edge window, Windows Firewall, RAM |
| `platform_other.go` | Linux/macOS: data folder, browser window, RAM |
| `firewall_linux.go` | ufw / firewalld via pkexec |
| `web/index.html` | The whole UI |
| `packaging/` | Icon, Linux desktop entry, installer, systemd unit |
| `scripts/package.sh` | Builds all release files into `dist/` |

## Build

```bash
./scripts/package.sh   # dist/MC-EasyHost.exe + Linux x86_64/aarch64 tarballs
```

Tests (the online test downloads Java and a server jar and checks the JVM flags):

```bash
go test ./...
EASYHOST_ONLINE=1 go test -run Online -v ./...
```

For development: `EASYHOST_NO_WINDOW=1 EASYHOST_DATA=/tmp/eh go run .`, then open http://127.0.0.1:47615.

Pushing a tag like `v1.2.0` makes GitHub Actions build every package and attach them to a release.

## License

MIT. Minecraft is a trademark of Mojang/Microsoft; this project is not affiliated with them. Running a server
requires agreeing to the [Minecraft EULA](https://aka.ms/MinecraftEULA).
