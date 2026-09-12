#!/bin/bash

set -euo pipefail

DIR=app/src/main/assets/sing-box
rm -rf "$DIR"
mkdir -p "$DIR"
cd "$DIR"

get_python() {
  if command -v python3 >/dev/null 2>&1 && python3 --version >/dev/null 2>&1; then
    echo python3
  elif command -v python >/dev/null 2>&1 && python --version >/dev/null 2>&1; then
    echo python
  else
    echo "Python is required to parse GitHub release metadata" >&2
    return 1
  fi
}

PYTHON_BIN="$(get_python)"

get_latest_release() {
  local repo="$1"
  local headers=(-H "Accept: application/vnd.github+json")
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    headers+=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
  fi
  curl --fail --silent --show-error --retry 3 "${headers[@]}" \
    "https://api.github.com/repos/${repo}/releases/latest" |
    "$PYTHON_BIN" -c 'import json,sys; value=json.load(sys.stdin).get("tag_name", ""); assert value, "missing tag_name"; print(value)'
}

download_asset() {
  local repo="$1"
  local version="$2"
  local asset="$3"
  test -n "$version"
  curl --fail --location --silent --show-error --retry 3 \
    --output "$asset" \
    "https://github.com/${repo}/releases/download/${version}/${asset}"
  test -s "$asset"
}

VERSION_GEOIP="$(get_latest_release "SagerNet/sing-geoip")"
echo "VERSION_GEOIP=$VERSION_GEOIP"
printf '%s' "$VERSION_GEOIP" > geoip.version.txt
download_asset "SagerNet/sing-geoip" "$VERSION_GEOIP" geoip.db
xz -9 geoip.db

VERSION_GEOSITE="$(get_latest_release "SagerNet/sing-geosite")"
echo "VERSION_GEOSITE=$VERSION_GEOSITE"
printf '%s' "$VERSION_GEOSITE" > geosite.version.txt
download_asset "SagerNet/sing-geosite" "$VERSION_GEOSITE" geosite.db
xz -9 geosite.db
