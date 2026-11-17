#!/usr/bin/env bash
# Builds every release artifact into dist/.
#   scripts/build.sh [version]
# Produces: Windows server+agent, Linux server+agent (amd64 and arm64),
#           macOS agent (arm64/amd64), and archive bundles for Linux.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${1:-$(grep -o 'Version = "[^"]*"' internal/shared/version/version.go | head -1 | cut -d'"' -f2)}"
OUT="dist"
rm -rf "$OUT"
mkdir -p "$OUT"

ldflags="-s -w -X github.com/pcim/pc-inventory-manager/internal/shared/version.Version=${VERSION}"
build() { # goos goarch pkg outfile
  echo "  $1/$2  $4"
  GOOS=$1 GOARCH=$2 CGO_ENABLED=0 go build -trimpath -ldflags "$ldflags" -o "$OUT/$4" "./$3"
}

echo "Building PC Inventory Manager ${VERSION}"
echo "Windows"
build windows amd64 cmd/server pcim-server.exe
build windows amd64 cmd/agent  pcim-agent.exe
echo "Linux"
build linux amd64 cmd/server pcim-server-linux-amd64
build linux amd64 cmd/agent  pcim-agent-linux-amd64
build linux arm64 cmd/server pcim-server-linux-arm64
build linux arm64 cmd/agent  pcim-agent-linux-arm64
echo "macOS"
build darwin arm64 cmd/agent pcim-agent-macos-arm64
build darwin amd64 cmd/agent pcim-agent-macos-amd64

# Linux release tarballs (binaries + docs + install script)
for arch in amd64 arm64; do
  pkg="$OUT/pcim-linux-$arch"
  mkdir -p "$pkg/docs"
  cp "$OUT/pcim-server-linux-$arch" "$pkg/pcim-server"
  cp "$OUT/pcim-agent-linux-$arch"  "$pkg/pcim-agent"
  cp README.md "$pkg/"
  cp docs/*.md "$pkg/docs/" 2>/dev/null || true
  cp packaging/linux/install.sh packaging/linux/pcim-server.service packaging/linux/pcim-agent.service "$pkg/"
  chmod +x "$pkg/install.sh" "$pkg/pcim-server" "$pkg/pcim-agent"
  ( cd "$OUT" && tar -czf "pcim-${VERSION}-linux-${arch}.tar.gz" "pcim-linux-$arch" )
  rm -rf "$pkg"
done

# Windows zip with both binaries (installer is built separately by Inno Setup)
mkdir -p "$OUT/pcim-windows"
cp "$OUT/pcim-server.exe" "$OUT/pcim-agent.exe" README.md "$OUT/pcim-windows/"
cp -r docs "$OUT/pcim-windows/" 2>/dev/null || true
( cd "$OUT" && powershell -NoProfile -Command "Compress-Archive -Force -Path 'pcim-windows' -DestinationPath 'pcim-${VERSION}-windows.zip'" 2>/dev/null || zip -qr "pcim-${VERSION}-windows.zip" pcim-windows )
rm -rf "$OUT/pcim-windows"

echo
echo "Artifacts in $OUT/:"
ls -lh "$OUT" | tail -n +2 | awk '{printf "  %-40s %s\n", $9, $5}'
