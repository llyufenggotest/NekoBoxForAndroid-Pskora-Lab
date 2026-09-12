#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"
GO_BIN="${GO_BIN:-go}"
export GOPATH="${GOPATH:-$($GO_BIN env GOPATH)}"
export GOBIN="${GOBIN:-$GOPATH/bin}"
export GOCACHE="${GOCACHE:-$HOME/.cache/go-build}"
export GOMODCACHE="${GOMODCACHE:-$GOPATH/pkg/mod}"
export GOTOOLCHAIN=local
export PATH="$GOBIN:$(dirname "$(command -v "$GO_BIN")"):$PATH"
EXE=""
case "$($GO_BIN env GOHOSTOS)" in windows) EXE=.exe;; esac
SOURCE="${GOMOBILE_SOURCE:-$PWD/../gomobile}"
mkdir -p "$GOBIN"
(
 cd "$SOURCE"
 "$GO_BIN" build -o "$GOBIN/gomobile-matsuri$EXE" ./cmd/gomobile
 "$GO_BIN" build -o "$GOBIN/gobind-matsuri$EXE" ./cmd/gobind
)
export GOBIND="$GOBIN/gobind-matsuri$EXE"
"$GOBIN/gomobile-matsuri$EXE" init
