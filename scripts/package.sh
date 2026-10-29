#!/bin/sh
# Builds every release file into dist/:
#   MC-EasyHost.exe                        Windows x64
#   mc-easyhost-linux-x86_64.tar.gz        Linux x64 (binary + installer)
#   mc-easyhost-linux-aarch64.tar.gz       Linux ARM64, e.g. Raspberry Pi 4/5
set -eu
cd "$(dirname "$0")/.."

version=${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}
ldflags="-s -w -X main.version=$version"
rm -rf dist && mkdir -p dist

echo "Building $version"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$ldflags -H windowsgui" -o dist/MC-EasyHost.exe .

for pair in amd64:x86_64 arm64:aarch64; do
	goarch=${pair%%:*}
	name=${pair##*:}
	dir="dist/mc-easyhost-linux-$name"
	mkdir -p "$dir"
	CGO_ENABLED=0 GOOS=linux GOARCH=$goarch go build -trimpath -ldflags "$ldflags" -o "$dir/mc-easyhost" .
	cp packaging/linux/install.sh packaging/linux/mc-easyhost.desktop packaging/linux/mc-easyhost.service "$dir/"
	cp packaging/icon.svg "$dir/mc-easyhost.svg"
	cp LICENSE "$dir/"
	tar -C dist -czf "dist/mc-easyhost-linux-$name.tar.gz" "mc-easyhost-linux-$name"
	rm -rf "$dir"
done

ls -lh dist
