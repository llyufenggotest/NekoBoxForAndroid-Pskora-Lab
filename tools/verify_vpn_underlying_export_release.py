#!/usr/bin/env python
"""Verify release routes VPNUNDERTRACE into exported neko.log."""
from __future__ import annotations

import argparse
import re
import subprocess
import tempfile
from pathlib import Path
from zipfile import ZipFile


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("apk", type=Path)
    parser.add_argument("--dexdump", default="G:/Android/Sdk/build-tools/35.0.0/dexdump.exe")
    args = parser.parse_args()

    with ZipFile(args.apk) as archive, tempfile.TemporaryDirectory() as temp:
        texts = []
        for name in archive.namelist():
            if re.fullmatch(r"classes\d*\.dex", name):
                dex = Path(temp, name)
                dex.write_bytes(archive.read(name))
                texts.append(subprocess.check_output([args.dexdump, "-d", str(dex)], text=True, errors="replace"))
        text = "\n".join(texts)

    assert "VPNUNDERTRACE schema=2" in text, "diagnostic schema missing"
    assert "VpnUnderlyingDiagnosticSink" in text, "persistent sink class missing"
    assert "Lio/nekohasekai/sagernet/ktx/Logs;.i:" in text, "NB4A Logs.i writer call missing"
    assert 'neko.log' in text, "exported persistent log filename missing"
    assert "Lmoe/matsuri/nb4a/utils/SendLog;.getNekoLog:" in text, "export reader entrypoint missing"
    assert "Landroid/util/Log;.i:" not in class_block(text, "VpnUnderlyingLiveDiagnostic"), "old logcat-only sink remains"
    print("PASS: Release DEX routes VPNUNDERTRACE through Logs.i and retains SendLog neko.log reader")


def class_block(text: str, needle: str) -> str:
    start = text.find(needle)
    if start < 0:
        return ""
    next_class = text.find("Class #", start + len(needle))
    return text[start: next_class if next_class >= 0 else None]


if __name__ == "__main__":
    main()
