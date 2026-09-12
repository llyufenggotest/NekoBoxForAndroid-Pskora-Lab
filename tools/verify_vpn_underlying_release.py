#!/usr/bin/env python3
"""Verify the minified release keeps the bounded VPNUNDERTRACE sink reachable."""

import argparse
import pathlib
import subprocess
import tempfile
import zipfile

OWNER = "Lio/nekohasekai/sagernet/bg/VpnUnderlyingLiveDiagnostic;"


def verify_dump(text: str) -> None:
    assert OWNER in text, "VpnUnderlyingLiveDiagnostic class missing from release DEX"
    assert 'const-string v0, "VPNUNDERTRACE"' in text, "VPNUNDERTRACE tag missing from release sink"
    assert "#int 512" in text, "512-event process bound missing from release sink"
    assert "Landroid/util/Log;.i:(Ljava/lang/String;Ljava/lang/String;)I" in text, "release emit has no Log.i call"
    assert ".recordConnectivityCallback:(ILandroid/net/Network;)V" in text, "callback diagnostic call site removed"
    assert ".recordLiveSet:(Landroid/net/Network;Lkotlin/jvm/functions/Function1;)V" in text, "live-set diagnostic call site removed"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("apk", type=pathlib.Path)
    parser.add_argument("--dexdump", type=pathlib.Path, required=True)
    args = parser.parse_args()
    assert args.apk.is_file(), f"APK not found: {args.apk}"
    assert args.dexdump.is_file(), f"dexdump not found: {args.dexdump}"

    dumps = []
    with tempfile.TemporaryDirectory(prefix="vpn-underlying-dex-") as temp:
        temp_dir = pathlib.Path(temp)
        with zipfile.ZipFile(args.apk) as archive:
            dex_names = sorted(name for name in archive.namelist() if name.startswith("classes") and name.endswith(".dex"))
            assert dex_names, "release APK contains no classes*.dex"
            for name in dex_names:
                dex = temp_dir / pathlib.Path(name).name
                dex.write_bytes(archive.read(name))
                result = subprocess.run(
                    [str(args.dexdump), "-d", str(dex)],
                    capture_output=True,
                    text=True,
                    encoding="utf-8",
                    errors="replace",
                    check=True,
                )
                dumps.append(result.stdout)
    verify_dump("\n".join(dumps))
    print("PASS: release DEX retains bounded VPNUNDERTRACE Log.i sink and both production call sites")


if __name__ == "__main__":
    main()
