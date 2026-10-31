#!/usr/bin/env bash
# OS ごとのバイナリを bin/ に作る。Windows では Git Bash から実行する。
set -euo pipefail
cd "$(dirname "$0")"
out=../bin
for target in windows/amd64 windows/arm64 darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do
  os=${target%/*}; arch=${target#*/}
  ext=""; [ "$os" = "windows" ] && ext=".exe"
  echo "build $os/$arch"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o "$out/canon-$os-$arch$ext" ./cmd/canon
done
# Windows では MCP サーバーの起動に拡張子付きの実行ファイルが要るため、bin/canon.exe も置く
cp "$out/canon-windows-amd64.exe" "$out/canon.exe"
