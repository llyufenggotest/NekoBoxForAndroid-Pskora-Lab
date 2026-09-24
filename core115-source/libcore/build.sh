#!/usr/bin/env bash
set -euo pipefail

source ../buildScript/init/env_ndk.sh
BUILD=".build"
rm -rf "$BUILD/android" "$BUILD/java" "$BUILD/javac-output" "$BUILD/src"
GO_BIN="${GO_BIN:-go}"
GOPATH="${GOPATH:-$($GO_BIN env GOPATH)}"
export GOBIND="${GOBIND:-$GOPATH/bin/gobind-matsuri}"
"$GO_BIN" test github.com/sagernet/sing-vmess/vless
"$GO_BIN" test ./protocol/oppa
"$GO_BIN" test ./tunnetcontrol
"$GO_BIN" test github.com/sagernet/sing-box/protocol/trojan
# gomobile excludes *_test.go files; diagnostics implementations must be ordinary Go files.
for source in ip_quality.go speedtest.go; do
 test -f "$source"
done
"$GOPATH/bin/gomobile-matsuri" bind -v -target=android/arm64 -androidapi 21 -cache "$(realpath "$BUILD")" -trimpath -ldflags='-s -w -X github.com/sagernet/sing-box/constant.Version=1.15.0-alpha.2-private-lab' -tags='with_gvisor,with_quic,with_wireguard,with_utls,with_clash_api' .
rm -f libcore-sources.jar
mkdir -p ../../app/libs
cp -f libcore.aar ../../app/libs/libcore.aar
printf '>> install %s\n' "$(realpath ../../app/libs/libcore.aar)"
