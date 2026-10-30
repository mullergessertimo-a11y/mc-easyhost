#!/bin/sh
# Installs MC EasyHost for the current user (no root needed):
#   ~/.local/bin/mc-easyhost, a menu entry and an icon.
# Run ./install.sh --uninstall to remove them again (your worlds stay in
# ~/.local/share/mc-easyhost).
set -eu

here=$(cd "$(dirname "$0")" && pwd)
bin="$HOME/.local/bin"
apps="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
icons="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/scalable/apps"

if [ "${1:-}" = "--uninstall" ]; then
	rm -f "$bin/mc-easyhost" "$apps/mc-easyhost.desktop" "$icons/mc-easyhost.svg"
	command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$apps" || true
	echo "MC EasyHost removed. Your worlds are still in ${XDG_DATA_HOME:-$HOME/.local/share}/mc-easyhost"
	exit 0
fi

mkdir -p "$bin" "$apps" "$icons"
install -m 755 "$here/mc-easyhost" "$bin/mc-easyhost"
install -m 644 "$here/mc-easyhost.svg" "$icons/mc-easyhost.svg"
sed "s|^Exec=.*|Exec=$bin/mc-easyhost|" "$here/mc-easyhost.desktop" > "$apps/mc-easyhost.desktop"
chmod 644 "$apps/mc-easyhost.desktop"
command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$apps" || true
command -v gtk-update-icon-cache >/dev/null 2>&1 && gtk-update-icon-cache -q "${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor" 2>/dev/null || true

echo "MC EasyHost installed. Start it from your app menu, or run: mc-easyhost"
case ":$PATH:" in *":$bin:"*) ;; *) echo "Note: $bin is not on your PATH; the menu entry works anyway." ;; esac
