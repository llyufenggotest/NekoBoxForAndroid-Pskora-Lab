#!/usr/bin/env bash

source "buildScript/init/env.sh"

# The private 1.15 tree exists only in the audited local snapshot.
if grep -q 'local-core115-snapshot' buildScript/lib/core/native-baseline.json; then
    printf '%s\n' 'core115: restore the verified local source snapshot; see NATIVE_RECOVERY.md. Remote base pins are insufficient.' >&2
    exit 1
fi

# fetch source
bash buildScript/lib/core/get_source.sh

[ -f libcore/go.mod ] || exit 1
cd libcore

./init.sh || exit 1
