#!/usr/bin/env bash
set -euo pipefail

mode="${1:?usage: run_positive_lifecycle_race.sh normal|low-memory}"
case "$mode" in
  normal)
    tags='with_gvisor,with_quic,with_wireguard,with_utls,with_clash_api'
    ;;
  low-memory)
    tags='with_gvisor,with_quic,with_wireguard,with_utls,with_clash_api,with_low_memory'
    ;;
  *)
    printf 'unknown mode: %s\n' "$mode" >&2
    exit 2
    ;;
esac

packages=(
  github.com/sagernet/sing-tun
  github.com/sagernet/sing-box/experimental/clashapi
  github.com/sagernet/sing-box/common/urltest
)

mapfile -t sing_tun_tests < <(
  cd core115-source/libcore
  go test -list '^Test' -tags="$tags" github.com/sagernet/sing-tun |
    sed -n '/^Test/p'
)

sequence_wrap_present=false
for test_name in "${sing_tun_tests[@]}"; do
  if [[ "$test_name" == TestGoKernelSequenceWrap ]]; then
    sequence_wrap_present=true
    break
  fi
done
if [[ "$sequence_wrap_present" != true ]]; then
  printf 'required TestGoKernelSequenceWrap is absent\n' >&2
  exit 1
fi
sing_tun_tests=("${sing_tun_tests[@]/TestGoKernelSequenceWrap}")

first_offload=-1
for index in "${!sing_tun_tests[@]}"; do
  if [[ "${sing_tun_tests[$index]}" == TestGoKernelPacketOffload ]]; then
    first_offload=$index
    break
  fi
done
if (( first_offload < 0 )); then
  printf 'required TestGoKernelPacketOffload is absent\n' >&2
  exit 1
fi

before=()
after=()
if (( first_offload > 0 )); then
  before=("${sing_tun_tests[@]:0:first_offload}")
fi
after=("${sing_tun_tests[@]:first_offload}")

run_sing_tun_group() {
  local label="$1"
  shift
  (( $# > 0 )) || return 0
  local regex
  regex="^($(IFS='|'; printf '%s' "$*"))$"
  (
    cd core115-source/libcore
    go test -race -json -count=1 -timeout=120s -tags="$tags" \
      -run "$regex" github.com/sagernet/sing-tun
  )
}

# Preserve every discovered top-level test exactly once. Splitting before the
# long Linux GSO/GRO matrix gives each non-overlapping group the original 120s
# package budget instead of increasing or weakening the timeout.
run_sing_tun_group before-offload "${before[@]}"
run_sing_tun_group offload-and-after "${after[@]}"

run_sequence_wrap_case() {
  local case_name="$1"
  (
    cd core115-source/libcore
    go test -race -json -count=1 -timeout=120s -tags="$tags" \
      -run "^TestGoKernelSequenceWrap/${case_name}$" github.com/sagernet/sing-tun
  )
}

# This fixture transfers more than 4 GiB per direction to cross the TCP
# sequence boundary. Run its six existing subtests separately so the original
# 120s per-process timeout remains strict without parallel flows competing for
# one shared package budget.
for sequence_wrap_case in \
  ipv6=false/mode=write \
  ipv6=false/mode=buffer \
  ipv6=false/mode=splice \
  ipv6=true/mode=write \
  ipv6=true/mode=buffer \
  ipv6=true/mode=splice; do
  run_sequence_wrap_case "$sequence_wrap_case"
done

(
  cd core115-source/libcore
  go test -race -json -count=1 -timeout=120s -tags="$tags" \
    "${packages[@]:1}"
)
