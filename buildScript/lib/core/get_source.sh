#!/bin/bash
set -e

source "buildScript/init/env.sh"
ENV_NB4A=1
source "buildScript/lib/core/get_source_env.sh"
pushd ..

####

if [ ! -d "sing-box" ]; then
  git clone --no-checkout https://github.com/llyufenggotest/sing-box.git
fi
pushd sing-box
git checkout "$COMMIT_SING_BOX"
popd

####

if [ ! -d "libneko" ]; then
  git clone --no-checkout https://github.com/starifly/libneko.git
fi
pushd libneko
git checkout "$COMMIT_LIBNEKO"
popd

####

if [ ! -d "sing-shadowsocks2" ]; then
  git clone --no-checkout https://github.com/llyufeng/sing-shadowsocks2.git
fi
pushd sing-shadowsocks2
git checkout "$COMMIT_SING_SHADOWSOCKS2"
popd

####

if [ ! -d "sing-vmess" ]; then
  git clone --no-checkout https://github.com/llyufenggotest/sing-vmess.git
fi
pushd sing-vmess
git checkout "$COMMIT_SING_VMESS"
popd

####

if [ ! -d "sing-anytls-local" ]; then
  git clone --no-checkout https://github.com/llyufeng/sing-anytls.git sing-anytls-local
fi
pushd sing-anytls-local
git checkout "$COMMIT_SING_ANYTLS"
popd

####

popd
