#!/usr/bin/env python3
"""Require a clean checkout with only the ignored runtime libcore AAR."""
import argparse
from pathlib import Path
import subprocess

ALLOWED_STATUS = "!! app/libs/libcore.aar"


def status_lines(root: Path) -> list[str]:
    result = subprocess.run(
        [
            "git",
            "-c",
            "core.fsmonitor=false",
            "-C",
            str(root),
            "status",
            "--porcelain=v1",
            "--ignored",
            "--untracked-files=all",
        ],
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    return [line for line in result.stdout.splitlines() if line]


def verify(root: Path) -> None:
    lines = status_lines(root)
    unexpected = [line for line in lines if line != ALLOWED_STATUS]
    if unexpected:
        raise ValueError(
            "Runtime provenance requires clean staged/unstaged tracked files and "
            "only ignored app/libs/libcore.aar; unexpected status: " + ", ".join(unexpected)
        )


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[3])
    args = parser.parse_args()
    try:
        verify(args.root.resolve())
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, "RUNTIME CLEAN GATE FAILED: " + str(error) + "\n")
    print("RUNTIME CLEAN GATE PASS")


if __name__ == "__main__":
    main()
