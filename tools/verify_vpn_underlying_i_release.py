#!/usr/bin/env python
"""Verify an I-variant release omits all framework underlying calls."""
from __future__ import annotations
import argparse
import re
import subprocess
import tempfile
from pathlib import Path
from zipfile import ZipFile


def main() -> None:
    p = argparse.ArgumentParser()
    p.add_argument("apk", type=Path)
    p.add_argument("--dexdump", default="G:/Android/Sdk/build-tools/35.0.0/dexdump.exe")
    a = p.parse_args()
    with ZipFile(a.apk) as z, tempfile.TemporaryDirectory() as td:
        dumps = []
        for name in z.namelist():
            if re.fullmatch(r"classes\d*\.dex", name):
                path = Path(td, name)
                path.write_bytes(z.read(name))
                dumps.append(subprocess.check_output([a.dexdump, "-d", str(path)], text=True, errors="replace"))
    text = "\n".join(dumps)
    assert "Landroid/net/VpnService$Builder;.setUnderlyingNetworks:" not in text, "Builder framework call remains"
    assert "Lio/nekohasekai/sagernet/bg/VpnService;.setUnderlyingNetworks:" not in text, "live framework call remains"
    assert ".recordInitialOmitted:" in text, "initial omission diagnostic unreachable"
    assert ".recordSuppressedEstablishedUpdate:" in text, "live suppression diagnostic unreachable"
    assert "VPNUNDERTRACE schema=2" in text, "numeric diagnostic schema missing"
    print("PASS: I Release DEX has no Builder/live framework underlying call and retains both diagnostics")


if __name__ == "__main__":
    main()
